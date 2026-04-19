package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sapihav/tavily-cli/internal/client"
	"github.com/spf13/cobra"
)

// envelope is the stable stdout shape for every successful CLI invocation.
// Fields are ordered intentionally: schema_version first so consumers can
// gate-check before parsing the rest. See CLAUDE.md "Output contract".
type envelope struct {
	SchemaVersion string `json:"schema_version"`
	Provider      string `json:"provider"`
	Command       string `json:"command"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	Result        any    `json:"result"`
}

// search-specific flags.
var (
	searchMaxResults              int
	searchDepth                   string
	searchTopic                   string
	searchIncludeAnswer           string // bare flag -> "basic" via NoOptDefVal
	searchTimeRange               string
	searchStartDate               string
	searchEndDate                 string
	searchIncludeDomains          []string
	searchExcludeDomains          []string
	searchCountry                 string
	searchIncludeImages            bool
	searchIncludeImageDescriptions bool
	searchIncludeRawContent        bool
	searchRawContentFormat         string
	searchIncludeFavicon           bool
	searchChunksPerSource         int
	searchAutoParameters          bool
	searchExactMatch              bool
	searchSafeSearch              bool
)

// Domain caps enforced client-side to fail fast with a clear error rather than
// waiting for the API's generic 400.
const (
	maxIncludeDomains = 300
	maxExcludeDomains = 150
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Run a Tavily search and emit JSON results",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	f := searchCmd.Flags()
	f.IntVar(&searchMaxResults, "max-results", 5, "Maximum results to return (0-20)")
	f.StringVar(&searchDepth, "search-depth", "basic", "Search depth: basic|advanced|fast|ultra-fast")
	f.StringVar(&searchTopic, "topic", "general", "Topic: general|news|finance")

	// --include-answer accepts either a bare flag (back-compat: bool→"basic")
	// or an explicit value (basic|advanced). NoOptDefVal lets `--include-answer`
	// with no value resolve to "basic".
	f.StringVar(&searchIncludeAnswer, "include-answer", "", "Include synthesized answer: basic|advanced (bare flag = basic)")
	f.Lookup("include-answer").NoOptDefVal = "basic"

	f.StringVar(&searchTimeRange, "time-range", "", "Time range filter: d|w|m|y")
	f.StringVar(&searchStartDate, "start-date", "", "Return results after this date (YYYY-MM-DD)")
	f.StringVar(&searchEndDate, "end-date", "", "Return results before this date (YYYY-MM-DD)")

	f.StringSliceVar(&searchIncludeDomains, "include-domain", nil, "Domain to include (repeatable, max 300)")
	f.StringSliceVar(&searchExcludeDomains, "exclude-domain", nil, "Domain to exclude (repeatable, max 150)")

	f.StringVar(&searchCountry, "country", "", "Boost results from country (topic=general only)")

	f.BoolVar(&searchIncludeImages, "include-images", false, "Include images in the response")
	f.BoolVar(&searchIncludeImageDescriptions, "include-image-descriptions", false, "Include descriptions for each image (requires --include-images)")

	f.BoolVar(&searchIncludeRawContent, "include-raw-content", false, "Include cleaned HTML content of each result")
	f.StringVar(&searchRawContentFormat, "raw-content-format", "markdown", "Raw content format: markdown|text (used with --include-raw-content)")

	f.BoolVar(&searchIncludeFavicon, "include-favicon", false, "Include favicon URL for each result")
	f.IntVar(&searchChunksPerSource, "chunks-per-source", 0, "Max chunks per source, 1-3 (advanced depth only)")
	f.BoolVar(&searchAutoParameters, "auto-parameters", false, "Let Tavily auto-configure parameters from the query")
	f.BoolVar(&searchExactMatch, "exact-match", false, "Only return results matching exact quoted phrases")
	f.BoolVar(&searchSafeSearch, "safe-search", false, "Filter adult/unsafe content (enterprise; not for fast/ultra-fast)")

	rootCmd.AddCommand(searchCmd)
}

// runSearch is the Cobra handler. It keeps all IO / exit-code mapping in one
// place so the client package stays transport-layer only.
func runSearch(cmd *cobra.Command, args []string) error {
	start := time.Now()
	query := args[0]

	req, err := buildSearchRequest(query)
	if err != nil {
		return userError(err)
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return userError(client.ErrMissingAPIKey)
	}

	c := client.New(apiKey, flagMaxRetries)

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "tavily: POST %s/search (query=%q, max_results=%d, depth=%s, topic=%s)\n",
			c.BaseURL, query, req.MaxResults, req.SearchDepth, req.Topic)
	}

	resp, err := c.Search(cmd.Context(), req)
	if err != nil {
		return err
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "search",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// buildSearchRequest validates every flag and packs the client request.
// All validation errors are user errors (exit 1).
func buildSearchRequest(query string) (client.SearchRequest, error) {
	var zero client.SearchRequest

	if err := validateEnum(searchDepth, "search-depth", "basic", "advanced", "fast", "ultra-fast"); err != nil {
		return zero, err
	}
	if err := validateEnum(searchTopic, "topic", "general", "news", "finance"); err != nil {
		return zero, err
	}
	if searchTimeRange != "" {
		if err := validateEnum(searchTimeRange, "time-range", "d", "w", "m", "y"); err != nil {
			return zero, err
		}
	}
	if err := validateDate(searchStartDate, "start-date"); err != nil {
		return zero, err
	}
	if err := validateDate(searchEndDate, "end-date"); err != nil {
		return zero, err
	}
	if len(searchIncludeDomains) > maxIncludeDomains {
		return zero, fmt.Errorf("--include-domain exceeds max %d (got %d)", maxIncludeDomains, len(searchIncludeDomains))
	}
	if len(searchExcludeDomains) > maxExcludeDomains {
		return zero, fmt.Errorf("--exclude-domain exceeds max %d (got %d)", maxExcludeDomains, len(searchExcludeDomains))
	}

	answer, err := normalizeAnswer(searchIncludeAnswer)
	if err != nil {
		return zero, err
	}
	raw, err := normalizeRawContent(searchIncludeRawContent, searchRawContentFormat)
	if err != nil {
		return zero, err
	}
	if searchIncludeImageDescriptions && !searchIncludeImages {
		return zero, fmt.Errorf("--include-image-descriptions requires --include-images")
	}

	return client.SearchRequest{
		Query:                    query,
		MaxResults:               searchMaxResults,
		SearchDepth:              searchDepth,
		Topic:                    searchTopic,
		TimeRange:                searchTimeRange,
		StartDate:                searchStartDate,
		EndDate:                  searchEndDate,
		IncludeDomains:           searchIncludeDomains,
		ExcludeDomains:           searchExcludeDomains,
		Country:                  searchCountry,
		IncludeImages:            searchIncludeImages,
		IncludeImageDescriptions: searchIncludeImageDescriptions,
		IncludeAnswer:            answer,
		IncludeRawContent:        raw,
		IncludeFavicon:           searchIncludeFavicon,
		ChunksPerSource:          searchChunksPerSource,
		AutoParameters:           searchAutoParameters,
		ExactMatch:               searchExactMatch,
		SafeSearch:               searchSafeSearch,
	}, nil
}

// normalizeAnswer maps flag input to the upstream JSON shape.
// Empty string -> omit (nil). "basic"/"advanced" pass through.
// "true"/"false" are accepted for back-compat with the prior boolean flag.
func normalizeAnswer(v string) (any, error) {
	switch strings.ToLower(v) {
	case "":
		return nil, nil
	case "true":
		return "basic", nil
	case "false":
		return nil, nil
	case "basic", "advanced":
		return v, nil
	default:
		return nil, fmt.Errorf("invalid --include-answer %q (allowed: basic, advanced)", v)
	}
}

// normalizeRawContent collapses --include-raw-content + --raw-content-format
// into the upstream JSON shape (string enum). Returns nil when raw content is
// not requested so the field is omitted from the payload.
func normalizeRawContent(include bool, format string) (any, error) {
	if !include {
		return nil, nil
	}
	switch strings.ToLower(format) {
	case "", "markdown":
		return "markdown", nil
	case "text":
		return "text", nil
	default:
		return nil, fmt.Errorf("invalid --raw-content-format %q (allowed: markdown, text)", format)
	}
}

// validateDate accepts "" (unset) or a strict YYYY-MM-DD string.
func validateDate(v, flag string) error {
	if v == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return fmt.Errorf("invalid --%s %q (expected YYYY-MM-DD)", flag, v)
	}
	return nil
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
