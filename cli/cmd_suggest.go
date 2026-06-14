package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) suggestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest <query>",
		Short: "Show Google search suggestions for a query",
		Example: `  google suggest golang
  google suggest "machine learning" --limit 5
  google suggest python -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			limit := a.effectiveLimit(10)
			a.progressf("fetching suggestions for %q", query)
			results, err := a.client.Suggest(cmd.Context(), query, limit)
			if err != nil {
				return codeError(exitError, err)
			}
			return a.renderOrEmpty(results, len(results))
		},
	}
	return cmd
}
