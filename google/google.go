// Package google is the library behind the google command line:
// HTTP client, typed models, and endpoint calls for Google's public data.
//
// The Client is the spine every command shares. It sets a real User-Agent,
// paces requests so a busy session stays polite, and retries transient
// failures (429 and 5xx). Build suggest and news calls on top of it.
package google

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to Google servers.
const DefaultUserAgent = "google-cli/dev (+https://github.com/tamnd/google-cli)"

// Config holds all tunable parameters for the client.
type Config struct {
	// BaseURL is the suggest base (default https://suggestqueries.google.com).
	BaseURL string
	// NewsBaseURL is the news RSS base (default https://news.google.com).
	NewsBaseURL string
	Rate        time.Duration
	Retries     int
	Timeout     time.Duration
	UserAgent   string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:     "https://suggestqueries.google.com",
		NewsBaseURL: "https://news.google.com",
		Rate:        200 * time.Millisecond,
		Retries:     5,
		Timeout:     30 * time.Second,
		UserAgent:   DefaultUserAgent,
	}
}

// Client talks to Google over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	last time.Time
}

// NewClient returns a Client configured from cfg.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// Suggestion is a single query suggestion returned by the Suggest endpoint.
type Suggestion struct {
	Rank  int    `json:"rank"`
	Query string `json:"query"`
}

// NewsItem is a single headline returned by the Google News RSS feed.
type NewsItem struct {
	Rank      int    `json:"rank"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	Published string `json:"published"`
	URL       string `json:"url"`
}

// Suggest fetches query suggestions for query from Google's suggest endpoint.
// Up to limit results are returned (0 = all returned by the server, max ~10).
func (c *Client) Suggest(ctx context.Context, query string, limit int) ([]Suggestion, error) {
	u := c.cfg.BaseURL + "/complete/search?client=firefox&q=" + url.QueryEscape(query)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("suggest: %w", err)
	}
	// Response: ["query", ["s1","s2",...], [], {...}]
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("suggest: parse JSON: %w", err)
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("suggest: unexpected response shape")
	}
	var suggestions []string
	if err := json.Unmarshal(raw[1], &suggestions); err != nil {
		return nil, fmt.Errorf("suggest: parse suggestions: %w", err)
	}
	out := make([]Suggestion, 0, len(suggestions))
	for i, s := range suggestions {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, Suggestion{Rank: i + 1, Query: s})
	}
	return out, nil
}

// rssChannel is the RSS response shape we parse.
type rssChannel struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title   string    `xml:"title"`
	Link    string    `xml:"link"`
	PubDate string    `xml:"pubDate"`
	Source  rssSource `xml:"source"`
}

type rssSource struct {
	Name string `xml:",chardata"`
}

// News fetches headlines from Google News RSS for the given query.
// query may be empty to get the current top headlines.
// Up to limit results are returned (0 = all returned by the server).
func (c *Client) News(ctx context.Context, query string, limit int) ([]NewsItem, error) {
	var u string
	if strings.TrimSpace(query) == "" {
		u = c.cfg.NewsBaseURL + "/rss?hl=en-US&gl=US&ceid=US:en"
	} else {
		u = c.cfg.NewsBaseURL + "/rss/search?q=" + url.QueryEscape(query) + "&hl=en-US&gl=US&ceid=US:en"
	}
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("news: %w", err)
	}
	var ch rssChannel
	if err := xml.Unmarshal(body, &ch); err != nil {
		return nil, fmt.Errorf("news: parse RSS: %w", err)
	}
	out := make([]NewsItem, 0, len(ch.Items))
	for i, it := range ch.Items {
		if limit > 0 && i >= limit {
			break
		}
		out = append(out, NewsItem{
			Rank:      i + 1,
			Title:     it.Title,
			Source:    it.Source.Name,
			Published: it.PubDate,
			URL:       it.Link,
		})
	}
	return out, nil
}

// get fetches url with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has elapsed since the last request.
func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
