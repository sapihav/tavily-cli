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

// extract-specific flags.
var (
	extractURLs            string
	extractQuery           string
	extractDepth           string
	extractFormat          string
	extractChunksPerSource int
	extractIncludeImages   bool
	extractIncludeFavicon  bool
	extractIncludeUsage    bool
	extractTimeoutSec      int
)

var extractCmd = &cobra.Command{
	Use:   "extract [url...]",
	Short: "Extract clean content from one or more URLs via POST /extract",
	Long: `Extract clean content from one or more URLs.

URLs can be provided as positional args, comma-separated via --urls, or
newline-separated on stdin (use "-" as a single arg). Sources can be mixed;
duplicates and blank lines are dropped while preserving first-seen order.

All URLs are sent in a single batched request — no per-URL fan-out.

Example:
  tavily extract https://example.com
  tavily extract https://a.example https://b.example --extract-depth advanced
  printf "https://x\nhttps://y\n" | tavily extract - --format text
  tavily extract --urls https://a,https://b --include-images --include-favicon`,
	RunE: runExtract,
}

func init() {
	f := extractCmd.Flags()
	f.StringVar(&extractURLs, "urls", "", "Comma-separated list of URLs (alternative to positional args)")
	f.StringVar(&extractQuery, "query", "", "Focus extraction on this query")
	f.StringVar(&extractDepth, "extract-depth", "basic", "Extract depth: basic|advanced")
	f.StringVar(&extractFormat, "format", "markdown", "Output format: markdown|text")
	f.IntVar(&extractChunksPerSource, "chunks-per-source", 0, "Max chunks per source, 1-5 (0 = server default)")
	f.BoolVar(&extractIncludeImages, "include-images", false, "Include image URLs extracted from each page")
	f.BoolVar(&extractIncludeFavicon, "include-favicon", false, "Include each source's favicon URL")
	f.BoolVar(&extractIncludeUsage, "include-usage", false, "Include per-request usage accounting in the response")
	f.IntVar(&extractTimeoutSec, "timeout", 60, "Per-request timeout in seconds")

	rootCmd.AddCommand(extractCmd)
}

// runExtract is the Cobra handler for `tavily extract`.
func runExtract(cmd *cobra.Command, args []string) error {
	start := time.Now()

	urls, err := collectExtractURLs(args, extractURLs, cmd.InOrStdin())
	if err != nil {
		return userError(err)
	}
	if len(urls) == 0 {
		return userError(fmt.Errorf("no URLs provided (pass as args, via --urls, or on stdin with '-')"))
	}

	req, err := buildExtractRequest(urls)
	if err != nil {
		return userError(err)
	}

	apiKey := os.Getenv("TAVILY_API_KEY")
	if apiKey == "" {
		return userError(client.ErrMissingAPIKey)
	}

	c := client.New(apiKey, flagMaxRetries)

	if flagVerbose && !flagQuiet {
		fmt.Fprintf(os.Stderr, "tavily: POST %s/extract (urls=%d, depth=%s, format=%s)\n",
			c.BaseURL, len(urls), req.ExtractDepth, req.Format)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(extractTimeoutSec)*time.Second)
	defer cancel()

	resp, err := c.Extract(ctx, req)
	if err != nil {
		return err
	}

	return writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "extract",
		ElapsedMs:     time.Since(start).Milliseconds(),
		Result:        resp,
	})
}

// buildExtractRequest validates flags and packs the client request. Validation
// failures are user errors (exit 2) so the CLI fails fast before touching the
// network.
func buildExtractRequest(urls []string) (client.ExtractRequest, error) {
	var zero client.ExtractRequest

	if err := validateEnum(extractDepth, "extract-depth", "basic", "advanced"); err != nil {
		return zero, err
	}
	if err := validateEnum(extractFormat, "format", "markdown", "text"); err != nil {
		return zero, err
	}
	if extractChunksPerSource != 0 && (extractChunksPerSource < 1 || extractChunksPerSource > 5) {
		return zero, fmt.Errorf("invalid --chunks-per-source %d (allowed: 1-5)", extractChunksPerSource)
	}
	if extractTimeoutSec <= 0 {
		return zero, fmt.Errorf("invalid --timeout %d (must be > 0)", extractTimeoutSec)
	}

	return client.ExtractRequest{
		URLs:            urls,
		Query:           extractQuery,
		ExtractDepth:    extractDepth,
		Format:          extractFormat,
		ChunksPerSource: extractChunksPerSource,
		IncludeImages:   extractIncludeImages,
		IncludeFavicon:  extractIncludeFavicon,
		IncludeUsage:    extractIncludeUsage,
	}, nil
}

// collectExtractURLs merges URLs from positional args, the --urls flag, and
// stdin (activated when any positional arg is literally "-"). Whitespace is
// trimmed, empty entries and duplicates are dropped while preserving
// first-seen order.
func collectExtractURLs(args []string, urlsFlag string, stdin io.Reader) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(args))

	add := func(raw string) {
		s := strings.TrimSpace(raw)
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	wantStdin := false
	for _, a := range args {
		if a == "-" {
			wantStdin = true
			continue
		}
		add(a)
	}

	if urlsFlag != "" {
		for _, part := range strings.Split(urlsFlag, ",") {
			add(part)
		}
	}

	if wantStdin {
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			add(sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
	}

	return out, nil
}
