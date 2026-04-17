package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sapihav/tavily-cli/internal/client"
	"github.com/spf13/cobra"
)

// search-specific flags.
var (
	searchMaxResults    int
	searchDepth         string
	searchTopic         string
	searchIncludeAnswer bool
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Run a Tavily search and emit JSON results",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().IntVar(&searchMaxResults, "max-results", 5, "Maximum results to return")
	searchCmd.Flags().StringVar(&searchDepth, "search-depth", "basic", "Search depth: basic|advanced")
	searchCmd.Flags().StringVar(&searchTopic, "topic", "general", "Topic: general|news")
	searchCmd.Flags().BoolVar(&searchIncludeAnswer, "include-answer", false, "Ask Tavily to include a synthesized answer")
	rootCmd.AddCommand(searchCmd)
}

// runSearch is the Cobra handler. It keeps all IO / exit-code mapping in one
// place so the client package stays transport-layer only.
func runSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	if err := validateEnum(searchDepth, "search-depth", "basic", "advanced"); err != nil {
		return userError(err)
	}
	if err := validateEnum(searchTopic, "topic", "general", "news"); err != nil {
		return userError(err)
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return userError(client.ErrMissingAPIKey)
	}

	c := client.New(apiKey, flagMaxRetries)

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "tavily: POST %s/search (query=%q, max_results=%d, depth=%s, topic=%s)\n",
			c.BaseURL, query, searchMaxResults, searchDepth, searchTopic)
	}

	resp, err := c.Search(cmd.Context(), client.SearchRequest{
		Query:         query,
		MaxResults:    searchMaxResults,
		SearchDepth:   searchDepth,
		Topic:         searchTopic,
		IncludeAnswer: searchIncludeAnswer,
	})
	if err != nil {
		return err
	}

	return writeJSON(resp)
}

// writeJSON marshals v and routes it to --out (if set) or stdout.
func writeJSON(v any) error {
	var buf []byte
	var err error
	if flagPretty {
		buf, err = json.MarshalIndent(v, "", "  ")
	} else {
		buf, err = json.Marshal(v)
	}
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}

	var w io.Writer = os.Stdout
	if flagOut != "" {
		f, err := os.Create(flagOut)
		if err != nil {
			return userError(fmt.Errorf("open --out file: %w", err))
		}
		defer f.Close()
		w = f
	}

	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// validateEnum returns an error naming the bad value; caller wraps as userError.
func validateEnum(got, flag string, allowed ...string) error {
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("invalid --%s %q (allowed: %v)", flag, got, allowed)
}

// userError marks a config / input error so main() can map to exit 2.
type userErr struct{ error }

func userError(err error) error { return &userErr{err} }

// IsUserError lets main() identify user errors without importing cmd internals.
func IsUserError(err error) bool { var u *userErr; return errors.As(err, &u) }
