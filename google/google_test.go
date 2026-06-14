package google_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tamnd/google-cli/google"
)

func newTestConfig(baseURL, newsBaseURL string) google.Config {
	cfg := google.DefaultConfig()
	cfg.BaseURL = baseURL
	cfg.NewsBaseURL = newsBaseURL
	cfg.Rate = 0
	cfg.Retries = 0
	return cfg
}

func TestSuggest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/complete/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Error(w, "missing q", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["golang",["golang tutorial","golang playground","golang download"],[],{}]`))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	c := google.NewClient(cfg)

	got, err := c.Suggest(context.Background(), "golang", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 suggestions, got %d", len(got))
	}
	if got[0].Query != "golang tutorial" {
		t.Errorf("first suggestion = %q, want %q", got[0].Query, "golang tutorial")
	}
	if got[0].Rank != 1 {
		t.Errorf("rank = %d, want 1", got[0].Rank)
	}
}

func TestSuggestLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["test",["a","b","c","d","e"],[],{}]`))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	c := google.NewClient(cfg)

	got, err := c.Suggest(context.Background(), "test", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 (limit), got %d", len(got))
	}
}

func TestNews(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Google News</title>
    <item>
      <title>Go 1.23 Released</title>
      <link>https://example.com/go123</link>
      <pubDate>Mon, 13 Jun 2026 12:00:00 GMT</pubDate>
      <source url="https://blog.golang.org">The Go Blog</source>
    </item>
    <item>
      <title>Go gains new features</title>
      <link>https://example.com/go-features</link>
      <pubDate>Sun, 12 Jun 2026 08:00:00 GMT</pubDate>
      <source url="https://techcrunch.com">TechCrunch</source>
    </item>
  </channel>
</rss>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rss))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	c := google.NewClient(cfg)

	got, err := c.News(context.Background(), "golang", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %d", len(got))
	}
	if got[0].Title != "Go 1.23 Released" {
		t.Errorf("title = %q, want %q", got[0].Title, "Go 1.23 Released")
	}
	if got[0].Source != "The Go Blog" {
		t.Errorf("source = %q, want %q", got[0].Source, "The Go Blog")
	}
	if got[0].Rank != 1 {
		t.Errorf("rank = %d, want 1", got[0].Rank)
	}
	if got[0].URL != "https://example.com/go123" {
		t.Errorf("url = %q", got[0].URL)
	}
}

func TestNewsLimit(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item><title>A</title><link>http://a.com</link><source>S</source></item>
    <item><title>B</title><link>http://b.com</link><source>S</source></item>
    <item><title>C</title><link>http://c.com</link><source>S</source></item>
    <item><title>D</title><link>http://d.com</link><source>S</source></item>
  </channel>
</rss>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(rss))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	c := google.NewClient(cfg)

	got, err := c.News(context.Background(), "test", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 (limit), got %d", len(got))
	}
}

func TestNewsEmptyQuery(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	c := google.NewClient(cfg)

	_, err := c.News(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/rss" {
		t.Errorf("empty query path = %q, want /rss", gotPath)
	}
}

func TestSuggestRetryOn503(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["go",["go lang"],[],{}]`))
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL, ts.URL)
	cfg.Retries = 5
	c := google.NewClient(cfg)

	got, err := c.Suggest(context.Background(), "go", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 suggestion after retry, got %d", len(got))
	}
}
