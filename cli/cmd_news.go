package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) newsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "news [query]",
		Short: "Show Google News headlines, optionally filtered by query",
		Example: `  google news
  google news "machine learning" --limit 5
  google news golang -o json
  google news "go programming" -o url`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var query string
			if len(args) > 0 {
				query = args[0]
			}
			limit := a.effectiveLimit(20)
			if query != "" {
				a.progressf("fetching news for %q", query)
			} else {
				a.progressf("fetching top headlines")
			}
			results, err := a.client.News(cmd.Context(), query, limit)
			if err != nil {
				return codeError(exitError, err)
			}
			return a.renderOrEmpty(results, len(results))
		},
	}
	return cmd
}
