package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sapihav/tavily-cli/internal/client"
	"github.com/spf13/cobra"
)

// crawl-specific flags.
//
// The flag surface is intentionally a union of `map` (graph traversal) and
// `extract` (per-page content). /crawl is the superset endpoint, so users
// don't need a separate `extract`-after-`map` pipeline.
var (
	crawlMaxDepth        int
	crawlMaxBreadth      int
	crawlLimit           int
	crawlInstructions    string
	crawlSelectPaths     []string
	crawlExcludePaths    []string
	crawlSelectDomains   []string
	crawlExcludeDomains  []string
	crawlAllowExternal   bool
	crawlExtractDepth    string
	crawlFormat          string
	crawlChunksPerSource int
	crawlIncludeImages   bool
	crawlIncludeFavicon  bool
	crawlIncludeUsage    bool
	crawlDryRun          bool
	crawlTimeoutSec      int
)

// newCrawlClient is the seam tests use to point the command at httptest.
// Defaults to client.New; tests swap and restore.
var newCrawlClient = client.New

var crawlCmd = &cobra.Command{
	Use:   "crawl [url]",
	Short: "BFS-crawl a site and extract per-page content via POST /crawl",
	Long: `Crawl a site starting from a root URL, returning extracted content per page.

/crawl is a superset of /map: same traversal flags (depth, breadth, limit,
filters) plus per-page extraction flags (extract-depth, format, images,
favicon). Pass "-" to read the URL from stdin (first non-blank line wins).

Example:
  tavily crawl https://docs.example.com --max-depth 2 --pretty
  tavily crawl https://x.example --select-path '^/docs/.*' --extract-depth advanced
  echo https://x.example | tavily crawl - --instructions "focus on the API reference"`,
	Args: cobra.ExactArgs(1),
	RunE: runCrawl,
}

func init() {
	f := crawlCmd.Flags()
	// Map-shaped flags (kept identical to `tavily map` for muscle memory).
	f.IntVar(&crawlMaxDepth, "max-depth", 0, "Max crawl depth from the root URL, 1-5 (0 = server default)")
	f.IntVar(&crawlMaxBreadth, "max-breadth", 0, "Max links to follow per page, 1-500 (0 = server default)")
	f.IntVar(&crawlLimit, "limit", 0, "Total link cap before stopping (0 = server default)")
	f.StringVar(&crawlInstructions, "instructions", "", "Natural-language guidance for the crawler")
	f.StringArrayVar(&crawlSelectPaths, "select-path", nil, "Regex of URL paths to include (repeatable)")
	f.StringArrayVar(&crawlExcludePaths, "exclude-path", nil, "Regex of URL paths to exclude (repeatable)")
	f.StringArrayVar(&crawlSelectDomains, "select-domain", nil, "Regex of domains/subdomains to include (repeatable)")
	f.StringArrayVar(&crawlExcludeDomains, "exclude-domain", nil, "Regex of domains/subdomains to exclude (repeatable)")
	f.BoolVar(&crawlAllowExternal, "allow-external", true, "Follow links to external domains (server default: true)")

	// Extract-shaped flags (server applies defaults when omitted).
	f.StringVar(&crawlExtractDepth, "extract-depth", "basic", "Extract depth: basic|advanced")
	f.StringVar(&crawlFormat, "format", "markdown", "Output format: markdown|text")
	f.IntVar(&crawlChunksPerSource, "chunks-per-source", 0, "Max chunks per source, 1-5 (0 = server default)")
	f.BoolVar(&crawlIncludeImages, "include-images", false, "Include image URLs extracted from each page")
	f.BoolVar(&crawlIncludeFavicon, "include-favicon", false, "Include each source's favicon URL")
	f.BoolVar(&crawlIncludeUsage, "include-usage", false, "Include per-request usage accounting in the response")

	f.BoolVar(&crawlDryRun, "dry-run", false, "Print the planned request body and exit; no network call")
	f.IntVar(&crawlTimeoutSec, "timeout", 150, "Per-request timeout in seconds")

	rootCmd.AddCommand(crawlCmd)
}

// runCrawl is the Cobra handler for `tavily crawl`.
func runCrawl(cmd *cobra.Command, args []string) error {
	start := time.Now()

	// resolveMapURL handles "-" → stdin first non-blank line. Reused so the
	// stdin contract is identical across map + crawl.
	url, err := resolveMapURL(args[0], cmd.InOrStdin())
	if err != nil {
		return userError(err)
	}

	req, err := buildCrawlRequest(url, cmd.Flags().Changed("allow-external"))
	if err != nil {
		return userError(err)
	}

	if crawlDryRun {
		// Echo the planned request as the result so callers can inspect what
		// would be sent. Bearer auth is the default path; api_key is never set
		// in body during dry-run (secret redaction by construction).
		return writeJSON(envelope{
			SchemaVersion: "1",
			Provider:      "tavily",
			Command:       "crawl",
			ElapsedMs:     time.Since(start).Milliseconds(),
			Result: map[string]any{
				"dry_run":  true,
				"endpoint": "POST /crawl",
				"request":  req,
			},
		})
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return userError(client.ErrMissingAPIKey)
	}

	c := newCrawlClient(apiKey, flagMaxRetries)

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "tavily: POST %s/crawl (url=%q, max_depth=%d, limit=%d, extract_depth=%s)\n",
			c.BaseURL, url, req.MaxDepth, req.Limit, req.ExtractDepth)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(crawlTimeoutSec)*time.Second)
	defer cancel()

	resp, err := c.Crawl(ctx, req)
	if err != nil {
		return err
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "crawl",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// buildCrawlRequest validates flags and packs the client request. Validation
// errors are user errors so the CLI fails fast before touching the network.
// allowExternalSet reports whether the caller explicitly set --allow-external;
// when false we omit the field so the server applies its own default.
func buildCrawlRequest(url string, allowExternalSet bool) (client.CrawlRequest, error) {
	var zero client.CrawlRequest

	if url == "" {
		return zero, fmt.Errorf("url is required")
	}
	if crawlMaxDepth < 0 || crawlMaxDepth > 5 {
		return zero, fmt.Errorf("invalid --max-depth %d (allowed: 1-5)", crawlMaxDepth)
	}
	if crawlMaxBreadth < 0 || crawlMaxBreadth > 500 {
		return zero, fmt.Errorf("invalid --max-breadth %d (allowed: 1-500)", crawlMaxBreadth)
	}
	if crawlLimit < 0 {
		return zero, fmt.Errorf("invalid --limit %d (must be >= 0)", crawlLimit)
	}
	if err := validateEnum(crawlExtractDepth, "extract-depth", "basic", "advanced"); err != nil {
		return zero, err
	}
	if err := validateEnum(crawlFormat, "format", "markdown", "text"); err != nil {
		return zero, err
	}
	if crawlChunksPerSource != 0 && (crawlChunksPerSource < 1 || crawlChunksPerSource > 5) {
		return zero, fmt.Errorf("invalid --chunks-per-source %d (allowed: 1-5)", crawlChunksPerSource)
	}
	// Upstream caps timeout at 150 seconds; reject obvious nonsense early.
	if crawlTimeoutSec <= 0 || crawlTimeoutSec > 150 {
		return zero, fmt.Errorf("invalid --timeout %d (allowed: 1-150)", crawlTimeoutSec)
	}

	req := client.CrawlRequest{
		URL:             url,
		MaxDepth:        crawlMaxDepth,
		MaxBreadth:      crawlMaxBreadth,
		Limit:           crawlLimit,
		Instructions:    crawlInstructions,
		SelectPaths:     crawlSelectPaths,
		ExcludePaths:    crawlExcludePaths,
		SelectDomains:   crawlSelectDomains,
		ExcludeDomains:  crawlExcludeDomains,
		ExtractDepth:    crawlExtractDepth,
		Format:          crawlFormat,
		ChunksPerSource: crawlChunksPerSource,
		IncludeImages:   crawlIncludeImages,
		IncludeFavicon:  crawlIncludeFavicon,
		IncludeUsage:    crawlIncludeUsage,
	}
	if allowExternalSet {
		v := crawlAllowExternal
		req.AllowExternal = &v
	}
	return req, nil
}
