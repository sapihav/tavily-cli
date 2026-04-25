package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sapihav/tavily-cli/internal/client"
	"github.com/spf13/cobra"
)

// map-specific flags.
var (
	mapMaxDepth       int
	mapMaxBreadth     int
	mapLimit          int
	mapInstructions   string
	mapSelectPaths    []string
	mapExcludePaths   []string
	mapSelectDomains  []string
	mapExcludeDomains []string
	mapAllowExternal  bool
	mapDryRun         bool
	mapTimeoutSec     int
)

var mapCmd = &cobra.Command{
	Use:   "map [url]",
	Short: "Map a site's URL graph via POST /map",
	Long: `Map a site's URL graph starting from a root URL.

The URL is a positional arg. Pass "-" to read the URL from stdin (first
non-blank line wins). Path/domain filters accept regex patterns and are
repeatable. --allow-external mirrors the upstream default (true).

Example:
  tavily map https://docs.example.com --max-depth 3 --pretty
  tavily map https://x.example --select-path '^/docs/.*' --exclude-path '^/admin/.*'
  echo https://x.example | tavily map - --instructions "focus on the API reference"`,
	Args: cobra.ExactArgs(1),
	RunE: runMap,
}

func init() {
	f := mapCmd.Flags()
	f.IntVar(&mapMaxDepth, "max-depth", 0, "Max crawl depth from the root URL (0 = server default)")
	f.IntVar(&mapMaxBreadth, "max-breadth", 0, "Max links to follow per page (0 = server default)")
	f.IntVar(&mapLimit, "limit", 0, "Total link cap before stopping (0 = server default)")
	f.StringVar(&mapInstructions, "instructions", "", "Natural-language guidance for the crawler")
	f.StringArrayVar(&mapSelectPaths, "select-path", nil, "Regex of URL paths to include (repeatable)")
	f.StringArrayVar(&mapExcludePaths, "exclude-path", nil, "Regex of URL paths to exclude (repeatable)")
	f.StringArrayVar(&mapSelectDomains, "select-domain", nil, "Regex of domains/subdomains to include (repeatable)")
	f.StringArrayVar(&mapExcludeDomains, "exclude-domain", nil, "Regex of domains/subdomains to exclude (repeatable)")
	f.BoolVar(&mapAllowExternal, "allow-external", true, "Follow links to external domains (server default: true)")
	f.BoolVar(&mapDryRun, "dry-run", false, "Print the planned request body and exit; no network call")
	f.IntVar(&mapTimeoutSec, "timeout", 60, "Per-request timeout in seconds")

	rootCmd.AddCommand(mapCmd)
}

// runMap is the Cobra handler for `tavily map`.
func runMap(cmd *cobra.Command, args []string) error {
	start := time.Now()

	url, err := resolveMapURL(args[0], cmd.InOrStdin())
	if err != nil {
		return userError(err)
	}

	req, err := buildMapRequest(url, cmd.Flags().Changed("allow-external"))
	if err != nil {
		return userError(err)
	}

	if mapDryRun {
		// Echo the planned request as the result so callers can inspect what
		// would be sent. Bearer auth is the default path; api_key is never set
		// in body during dry-run (secret redaction by construction).
		return writeJSON(envelope{
			SchemaVersion: "1",
			Provider:      "tavily",
			Command:       "map",
			ElapsedMs:     time.Since(start).Milliseconds(),
			Result: map[string]any{
				"dry_run":  true,
				"endpoint": "POST /map",
				"request":  req,
			},
		})
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return userError(client.ErrMissingAPIKey)
	}

	c := client.New(apiKey, flagMaxRetries)

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "tavily: POST %s/map (url=%q, max_depth=%d, limit=%d)\n",
			c.BaseURL, url, req.MaxDepth, req.Limit)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(mapTimeoutSec)*time.Second)
	defer cancel()

	resp, err := c.Map(ctx, req)
	if err != nil {
		return err
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "map",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// buildMapRequest validates flags and packs the client request. Validation
// errors are user errors so the CLI fails fast before touching the network.
// allowExternalSet reports whether the caller explicitly set --allow-external;
// when false we omit the field so the server applies its own default.
func buildMapRequest(url string, allowExternalSet bool) (client.MapRequest, error) {
	var zero client.MapRequest

	if url == "" {
		return zero, fmt.Errorf("url is required")
	}
	if mapMaxDepth < 0 || mapMaxDepth > 5 {
		return zero, fmt.Errorf("invalid --max-depth %d (allowed: 1-5)", mapMaxDepth)
	}
	if mapMaxBreadth < 0 || mapMaxBreadth > 500 {
		return zero, fmt.Errorf("invalid --max-breadth %d (allowed: 1-500)", mapMaxBreadth)
	}
	if mapLimit < 0 {
		return zero, fmt.Errorf("invalid --limit %d (must be >= 0)", mapLimit)
	}
	if mapTimeoutSec <= 0 {
		return zero, fmt.Errorf("invalid --timeout %d (must be > 0)", mapTimeoutSec)
	}

	req := client.MapRequest{
		URL:            url,
		MaxDepth:       mapMaxDepth,
		MaxBreadth:     mapMaxBreadth,
		Limit:          mapLimit,
		Instructions:   mapInstructions,
		SelectPaths:    mapSelectPaths,
		ExcludePaths:   mapExcludePaths,
		SelectDomains:  mapSelectDomains,
		ExcludeDomains: mapExcludeDomains,
	}
	if allowExternalSet {
		v := mapAllowExternal
		req.AllowExternal = &v
	}
	return req, nil
}

// resolveMapURL returns the positional URL, replacing "-" with the first
// non-blank line read from stdin.
func resolveMapURL(arg string, stdin io.Reader) (string, error) {
	if arg != "-" {
		return strings.TrimSpace(arg), nil
	}
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s != "" {
			return s, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return "", fmt.Errorf("no URL found on stdin")
}
