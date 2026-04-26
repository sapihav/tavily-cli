package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sapihav/tavily-cli/internal/client"
)

// resetCrawlFlags zeroes every package-level crawl flag so each test starts
// from the same baseline (cobra binds to globals).
func resetCrawlFlags() {
	crawlMaxDepth = 0
	crawlMaxBreadth = 0
	crawlLimit = 0
	crawlInstructions = ""
	crawlSelectPaths = nil
	crawlExcludePaths = nil
	crawlSelectDomains = nil
	crawlExcludeDomains = nil
	crawlAllowExternal = true
	crawlExtractDepth = "basic"
	crawlFormat = "markdown"
	crawlChunksPerSource = 0
	crawlIncludeImages = false
	crawlIncludeFavicon = false
	crawlIncludeUsage = false
	crawlDryRun = false
	crawlTimeoutSec = 150
}

func TestBuildCrawlRequest_Defaults(t *testing.T) {
	resetCrawlFlags()
	req, err := buildCrawlRequest("https://x", false)
	if err != nil {
		t.Fatalf("buildCrawlRequest: %v", err)
	}
	if req.URL != "https://x" {
		t.Errorf("URL = %q", req.URL)
	}
	// allow_external is omitted from the wire format unless the user
	// explicitly set the flag — preserves the server default (true).
	if req.AllowExternal != nil {
		t.Errorf("AllowExternal should be nil when flag not set, got %v", *req.AllowExternal)
	}
	if req.ExtractDepth != "basic" || req.Format != "markdown" {
		t.Errorf("default extract_depth/format wrong: %q / %q", req.ExtractDepth, req.Format)
	}
}

// Wire-format pin: every flag set, JSON body checked field-by-field.
func TestBuildCrawlRequest_AllFlags(t *testing.T) {
	resetCrawlFlags()
	crawlMaxDepth = 3
	crawlMaxBreadth = 50
	crawlLimit = 200
	crawlInstructions = "focus on api docs"
	crawlSelectPaths = []string{`^/docs/.*`}
	crawlExcludePaths = []string{`^/admin/.*`, `^/private/.*`}
	crawlSelectDomains = []string{`^docs\.example\.com$`}
	crawlExcludeDomains = []string{`^private\.example\.com$`}
	crawlAllowExternal = false
	crawlExtractDepth = "advanced"
	crawlFormat = "text"
	crawlChunksPerSource = 4
	crawlIncludeImages = true
	crawlIncludeFavicon = true
	crawlIncludeUsage = true

	req, err := buildCrawlRequest("https://example.com", true)
	if err != nil {
		t.Fatalf("buildCrawlRequest: %v", err)
	}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]any{
		"url":               "https://example.com",
		"max_depth":         float64(3),
		"max_breadth":       float64(50),
		"limit":             float64(200),
		"instructions":      "focus on api docs",
		"select_paths":      []any{"^/docs/.*"},
		"exclude_paths":     []any{"^/admin/.*", "^/private/.*"},
		"select_domains":    []any{`^docs\.example\.com$`},
		"exclude_domains":   []any{`^private\.example\.com$`},
		"allow_external":    false,
		"extract_depth":     "advanced",
		"format":            "text",
		"chunks_per_source": float64(4),
		"include_images":    true,
		"include_favicon":   true,
		"include_usage":     true,
	}
	if len(parsed) != len(want) {
		t.Errorf("field count mismatch: got %d want %d\n  got: %v\n  want: %v",
			len(parsed), len(want), parsed, want)
	}
	for k, v := range want {
		ab, _ := json.Marshal(parsed[k])
		bb, _ := json.Marshal(v)
		if string(ab) != string(bb) {
			t.Errorf("%s: got %s want %s", k, ab, bb)
		}
	}
	if strings.Contains(string(got), "api_key") {
		t.Errorf("api_key must not appear in wire payload: %s", got)
	}
}

func TestBuildCrawlRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"empty url", func() {}, "url"},
		{"depth too high", func() { crawlMaxDepth = 6 }, "max-depth"},
		{"depth negative", func() { crawlMaxDepth = -1 }, "max-depth"},
		{"breadth too high", func() { crawlMaxBreadth = 501 }, "max-breadth"},
		{"breadth negative", func() { crawlMaxBreadth = -1 }, "max-breadth"},
		{"limit negative", func() { crawlLimit = -5 }, "limit"},
		{"bad extract-depth", func() { crawlExtractDepth = "deep" }, "extract-depth"},
		{"bad format", func() { crawlFormat = "html" }, "format"},
		{"bad chunks low", func() { crawlChunksPerSource = -1 }, "chunks-per-source"},
		{"bad chunks high", func() { crawlChunksPerSource = 9 }, "chunks-per-source"},
		{"timeout zero", func() { crawlTimeoutSec = 0 }, "timeout"},
		{"timeout too high", func() { crawlTimeoutSec = 200 }, "timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetCrawlFlags()
			c.setup()
			url := "https://x"
			if c.name == "empty url" {
				url = ""
			}
			_, err := buildCrawlRequest(url, false)
			if err == nil {
				t.Fatalf("expected error mentioning %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

// Wire-format round-trip against a mock server: request body + response decode.
func TestCrawl_MockServer_Golden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/crawl" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if parsed["url"] != "https://example.com" {
			t.Errorf("url = %v", parsed["url"])
		}
		if parsed["max_depth"] != float64(2) {
			t.Errorf("max_depth = %v", parsed["max_depth"])
		}
		if parsed["extract_depth"] != "advanced" {
			t.Errorf("extract_depth = %v", parsed["extract_depth"])
		}
		if _, hasKey := parsed["api_key"]; hasKey {
			t.Errorf("api_key should not appear on Bearer path")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"base_url":"example.com",
			"results":[
				{"url":"https://example.com/a","raw_content":"page A","favicon":"https://example.com/fav.ico"},
				{"url":"https://example.com/b","raw_content":"page B","images":["https://example.com/img.png"]}
			],
			"response_time":2.34,
			"request_id":"rid-crawl",
			"usage":{"credits":4}
		}`))
	}))
	defer srv.Close()

	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	resp, err := c.Crawl(context.Background(), client.CrawlRequest{
		URL:          "https://example.com",
		MaxDepth:     2,
		ExtractDepth: "advanced",
	})
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if resp.BaseURL != "example.com" {
		t.Errorf("base_url = %q", resp.BaseURL)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results len = %d", len(resp.Results))
	}
	if resp.Results[0].URL != "https://example.com/a" || resp.Results[0].RawContent != "page A" {
		t.Errorf("results[0] = %+v", resp.Results[0])
	}
	if resp.Results[0].Favicon != "https://example.com/fav.ico" {
		t.Errorf("favicon missing: %+v", resp.Results[0])
	}
	if len(resp.Results[1].Images) != 1 || resp.Results[1].Images[0] != "https://example.com/img.png" {
		t.Errorf("images: %+v", resp.Results[1].Images)
	}
	if resp.ResponseTime != 2.34 || resp.RequestID != "rid-crawl" {
		t.Errorf("metadata: %+v", resp)
	}
	if resp.Usage["credits"] != float64(4) {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestCrawl_4xxNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad`))
	}))
	defer srv.Close()

	c := client.New("test-key", 3)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	_, err := c.Crawl(context.Background(), client.CrawlRequest{URL: "https://x"})
	var apiErr *client.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("expected APIError 400, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", calls.Load())
	}
}

func TestRunCrawl_RequiresArg(t *testing.T) {
	resetCrawlFlags()
	if err := crawlCmd.Args(crawlCmd, nil); err == nil {
		t.Errorf("expected ExactArgs(1) to reject 0 args")
	}
	if err := crawlCmd.Args(crawlCmd, []string{"a", "b"}); err == nil {
		t.Errorf("expected ExactArgs(1) to reject 2 args")
	}
	if err := crawlCmd.Args(crawlCmd, []string{"https://x"}); err != nil {
		t.Errorf("ExactArgs(1) rejected 1 arg: %v", err)
	}
}

func TestRunCrawl_MissingAPIKey(t *testing.T) {
	resetCrawlFlags()
	t.Setenv("TAVILY_API_KEY", "")
	err := runCrawl(crawlCmd, []string{"https://x"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on missing key, got %v", err)
	}
}

func TestRunCrawl_BadFlag(t *testing.T) {
	resetCrawlFlags()
	crawlMaxDepth = 99
	t.Setenv("TAVILY_API_KEY", "k")
	err := runCrawl(crawlCmd, []string{"https://x"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error, got %v", err)
	}
}

func TestRunCrawl_BadStdin(t *testing.T) {
	resetCrawlFlags()
	crawlDryRun = true
	t.Setenv("TAVILY_API_KEY", "k")
	crawlCmd.SetIn(strings.NewReader("\n  \n"))
	defer crawlCmd.SetIn(nil)

	err := runCrawl(crawlCmd, []string{"-"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on empty stdin, got %v", err)
	}
}

func TestRunCrawl_DryRunNoNetwork(t *testing.T) {
	resetCrawlFlags()
	crawlDryRun = true
	crawlMaxDepth = 2
	crawlInstructions = "secret-instr"
	crawlExtractDepth = "advanced"
	crawlIncludeUsage = true
	t.Setenv("TAVILY_API_KEY", "should-not-leak")

	// Capture stdout.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	err := runCrawl(crawlCmd, []string{"https://x"})
	if err != nil {
		t.Fatalf("runCrawl dry-run: %v", err)
	}
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	var env envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v\nstdout=%s", err, out)
	}
	if env.Command != "crawl" {
		t.Errorf("command = %q", env.Command)
	}
	resMap, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", env.Result)
	}
	if resMap["dry_run"] != true {
		t.Errorf("dry_run flag missing in output: %v", resMap)
	}
	if resMap["endpoint"] != "POST /crawl" {
		t.Errorf("endpoint = %v", resMap["endpoint"])
	}
	if strings.Contains(out, "should-not-leak") {
		t.Errorf("api key leaked into dry-run output: %s", out)
	}
	// extract-shaped flags must surface in the planned request.
	for _, sub := range []string{`"extract_depth":"advanced"`, `"include_usage":true`, `"max_depth":2`} {
		if !strings.Contains(out, sub) {
			t.Errorf("dry-run output missing %s\n  full=%s", sub, out)
		}
	}
}

func TestRunCrawl_StdinURL(t *testing.T) {
	resetCrawlFlags()
	crawlDryRun = true // avoid network
	t.Setenv("TAVILY_API_KEY", "k")
	crawlCmd.SetIn(strings.NewReader("https://from-stdin\n"))

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig; crawlCmd.SetIn(nil) }()

	if err := runCrawl(crawlCmd, []string{"-"}); err != nil {
		t.Fatalf("runCrawl: %v", err)
	}
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if !strings.Contains(buf.String(), "https://from-stdin") {
		t.Errorf("stdin URL not echoed in dry-run: %s", buf.String())
	}
}

// Drives the full runCrawl happy path through an httptest server: covers
// verbose stderr, c.Crawl call, and the success-branch writeJSON envelope.
func TestRunCrawl_HappyPath(t *testing.T) {
	resetCrawlFlags()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crawl" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"base_url":"example.com",
			"results":[{"url":"https://example.com/a","raw_content":"A"}],
			"response_time":0.5,
			"request_id":"rid"
		}`))
	}))
	defer srv.Close()

	// Inject a client pointed at the mock.
	prev := newCrawlClient
	newCrawlClient = func(apiKey string, maxRetries int) *client.Client {
		c := client.New(apiKey, maxRetries)
		c.BaseURL = srv.URL
		c.BackoffBase = time.Millisecond
		return c
	}
	defer func() { newCrawlClient = prev }()

	t.Setenv("TAVILY_API_KEY", "k")
	flagVerbose = true
	defer func() { flagVerbose = false }()
	crawlCmd.SetContext(context.Background())

	// Capture stdout + stderr.
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	if err := runCrawl(crawlCmd, []string{"https://example.com"}); err != nil {
		t.Fatalf("runCrawl: %v", err)
	}
	wOut.Close()
	wErr.Close()
	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)

	var env envelope
	if err := json.Unmarshal(outBuf.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v\nstdout=%s", err, outBuf.String())
	}
	if env.Provider != "tavily" || env.Command != "crawl" {
		t.Errorf("envelope = %+v", env)
	}
	if !strings.Contains(errBuf.String(), "POST ") || !strings.Contains(errBuf.String(), "/crawl") {
		t.Errorf("verbose stderr missing POST log: %q", errBuf.String())
	}
}

// Network success but server returns 500 forever — covers the c.Crawl error
// return inside runCrawl.
func TestRunCrawl_APIErrorBubble(t *testing.T) {
	resetCrawlFlags()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	prev := newCrawlClient
	newCrawlClient = func(apiKey string, maxRetries int) *client.Client {
		c := client.New(apiKey, 0)
		c.BaseURL = srv.URL
		c.BackoffBase = time.Millisecond
		return c
	}
	defer func() { newCrawlClient = prev }()

	t.Setenv("TAVILY_API_KEY", "k")
	crawlCmd.SetContext(context.Background())

	err := runCrawl(crawlCmd, []string{"https://example.com"})
	if err == nil {
		t.Fatal("expected API error, got nil")
	}
	var apiErr *client.APIError
	if !asAPIError(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Errorf("expected APIError 500, got %v", err)
	}
}
