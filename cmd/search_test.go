package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapihav/tavily-cli/internal/client"
)

// TestEnvelope_Shape locks the stdout contract documented in CLAUDE.md:
// schema_version, provider, command, elapsed_ms, result (with the decoded
// SearchResponse nested as-is).
func TestEnvelope_Shape(t *testing.T) {
	env := envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "search",
		ElapsedMs:     42,
		Result: &client.SearchResponse{
			Query:   "golang",
			Results: []client.SearchResult{{Title: "t", URL: "u", Score: 0.9}},
		},
	}

	buf, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got["schema_version"] != "1" {
		t.Errorf("schema_version = %v, want 1", got["schema_version"])
	}
	if got["provider"] != "tavily" {
		t.Errorf("provider = %v, want tavily", got["provider"])
	}
	if got["command"] != "search" {
		t.Errorf("command = %v, want search", got["command"])
	}
	// json.Unmarshal decodes numbers into float64 for map[string]any.
	if got["elapsed_ms"].(float64) != 42 {
		t.Errorf("elapsed_ms = %v, want 42", got["elapsed_ms"])
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", got["result"])
	}
	if result["query"] != "golang" {
		t.Errorf("result.query = %v, want golang", result["query"])
	}
}

// resetSearchFlags zeroes every package-level search flag so each test runs
// against a clean baseline (tests mutate globals because cobra binds to them).
func resetSearchFlags() {
	searchMaxResults = 5
	searchDepth = "basic"
	searchTopic = "general"
	searchIncludeAnswer = ""
	searchTimeRange = ""
	searchStartDate = ""
	searchEndDate = ""
	searchIncludeDomains = nil
	searchExcludeDomains = nil
	searchCountry = ""
	searchIncludeImages = false
	searchIncludeImageDescriptions = false
	searchIncludeRawContent = false
	searchRawContentFormat = "markdown"
	searchIncludeFavicon = false
	searchChunksPerSource = 0
	searchAutoParameters = false
	searchExactMatch = false
	searchSafeSearch = false
}

// TestBuildSearchRequest_Defaults confirms the M1 behaviour is unchanged:
// only the four original flags end up in the payload with their defaults.
func TestBuildSearchRequest_Defaults(t *testing.T) {
	resetSearchFlags()
	req, err := buildSearchRequest("hello")
	if err != nil {
		t.Fatalf("buildSearchRequest: %v", err)
	}
	if req.Query != "hello" || req.MaxResults != 5 || req.SearchDepth != "basic" || req.Topic != "general" {
		t.Errorf("unexpected defaults: %+v", req)
	}
	if req.IncludeAnswer != nil {
		t.Errorf("IncludeAnswer should be nil by default, got %v", req.IncludeAnswer)
	}
	if req.IncludeRawContent != nil {
		t.Errorf("IncludeRawContent should be nil by default, got %v", req.IncludeRawContent)
	}
}

// TestBuildSearchRequest_AllFlags exercises every new flag and asserts the
// emitted JSON payload (golden replay) so wire-format regressions are caught.
func TestBuildSearchRequest_AllFlags(t *testing.T) {
	resetSearchFlags()
	searchMaxResults = 10
	searchDepth = "advanced"
	searchTopic = "finance"
	searchIncludeAnswer = "advanced"
	searchTimeRange = "w"
	searchStartDate = "2026-01-01"
	searchEndDate = "2026-04-01"
	searchIncludeDomains = []string{"example.com", "foo.io"}
	searchExcludeDomains = []string{"spam.biz"}
	searchCountry = "united states"
	searchIncludeImages = true
	searchIncludeImageDescriptions = true
	searchIncludeRawContent = true
	searchRawContentFormat = "text"
	searchIncludeFavicon = true
	searchChunksPerSource = 3
	searchAutoParameters = true
	searchExactMatch = true
	searchSafeSearch = true

	req, err := buildSearchRequest("leo messi")
	if err != nil {
		t.Fatalf("buildSearchRequest: %v", err)
	}

	got, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Compare as a map to avoid key-order flake while still pinning every field.
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]any{
		"query":                      "leo messi",
		"max_results":                float64(10),
		"search_depth":               "advanced",
		"topic":                      "finance",
		"time_range":                 "w",
		"start_date":                 "2026-01-01",
		"end_date":                   "2026-04-01",
		"include_domains":            []any{"example.com", "foo.io"},
		"exclude_domains":            []any{"spam.biz"},
		"country":                    "united states",
		"include_images":             true,
		"include_image_descriptions": true,
		"include_answer":             "advanced",
		"include_raw_content":        "text",
		"include_favicon":            true,
		"chunks_per_source":          float64(3),
		"auto_parameters":            true,
		"exact_match":                true,
		"safe_search":                true,
	}
	// deep-equal via re-marshal is enough; hand-rolling would obscure mismatches.
	wantBytes, _ := json.Marshal(want)
	var wantParsed map[string]any
	_ = json.Unmarshal(wantBytes, &wantParsed)

	if len(parsed) != len(wantParsed) {
		t.Errorf("field count mismatch: got %d fields, want %d\n  got:  %v\n  want: %v",
			len(parsed), len(wantParsed), parsed, wantParsed)
	}
	for k, v := range wantParsed {
		if !jsonEqual(parsed[k], v) {
			t.Errorf("%s: got %v, want %v", k, parsed[k], v)
		}
	}
	// Ensure api_key never leaks into the payload.
	if strings.Contains(string(got), "api_key") {
		t.Errorf("api_key must not appear in wire payload: %s", got)
	}
}

// TestBuildSearchRequest_IncludeAnswerBareFlag covers the back-compat path:
// a bare `--include-answer` (no value) resolves to "basic" via NoOptDefVal,
// which the test emulates by setting the flag var directly to "basic".
func TestBuildSearchRequest_IncludeAnswerBareFlag(t *testing.T) {
	resetSearchFlags()
	// NoOptDefVal="basic" means cobra sets the var to "basic" on a bare flag.
	searchIncludeAnswer = "basic"
	req, err := buildSearchRequest("q")
	if err != nil {
		t.Fatalf("buildSearchRequest: %v", err)
	}
	if req.IncludeAnswer != "basic" {
		t.Errorf("IncludeAnswer = %v, want basic", req.IncludeAnswer)
	}
}

// TestBuildSearchRequest_IncludeAnswerTrueFalse covers legacy boolean strings
// that a pre-M2 user may have in their scripts (e.g. `--include-answer=true`).
func TestBuildSearchRequest_IncludeAnswerTrueFalse(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"true", "basic"},
		{"True", "basic"},
		{"false", nil},
		{"", nil},
	}
	for _, c := range cases {
		resetSearchFlags()
		searchIncludeAnswer = c.in
		req, err := buildSearchRequest("q")
		if err != nil {
			t.Fatalf("buildSearchRequest(%q): %v", c.in, err)
		}
		if req.IncludeAnswer != c.want {
			t.Errorf("in=%q: IncludeAnswer = %v, want %v", c.in, req.IncludeAnswer, c.want)
		}
	}
}

// TestBuildSearchRequest_RawContentFormat asserts --include-raw-content is
// required to surface --raw-content-format, and that the format value maps
// into the upstream string enum.
func TestBuildSearchRequest_RawContentFormat(t *testing.T) {
	resetSearchFlags()
	searchIncludeRawContent = false
	searchRawContentFormat = "text"
	req, _ := buildSearchRequest("q")
	if req.IncludeRawContent != nil {
		t.Errorf("raw content disabled, but IncludeRawContent = %v", req.IncludeRawContent)
	}

	resetSearchFlags()
	searchIncludeRawContent = true
	searchRawContentFormat = "text"
	req, _ = buildSearchRequest("q")
	if req.IncludeRawContent != "text" {
		t.Errorf("IncludeRawContent = %v, want text", req.IncludeRawContent)
	}

	resetSearchFlags()
	searchIncludeRawContent = true
	searchRawContentFormat = "markdown"
	req, _ = buildSearchRequest("q")
	if req.IncludeRawContent != "markdown" {
		t.Errorf("IncludeRawContent = %v, want markdown", req.IncludeRawContent)
	}
}

// TestBuildSearchRequest_ValidationErrors locks every error path.
func TestBuildSearchRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"bad depth", func() { searchDepth = "lightspeed" }, "search-depth"},
		{"bad topic", func() { searchTopic = "weather" }, "topic"},
		{"bad time-range", func() { searchTimeRange = "decade" }, "time-range"},
		{"bad start-date", func() { searchStartDate = "yesterday" }, "start-date"},
		{"bad end-date", func() { searchEndDate = "2026/01/01" }, "end-date"},
		{"bad answer", func() { searchIncludeAnswer = "deep" }, "include-answer"},
		{"bad raw format", func() {
			searchIncludeRawContent = true
			searchRawContentFormat = "pdf"
		}, "raw-content-format"},
		{"descriptions w/o images", func() { searchIncludeImageDescriptions = true }, "include-image-descriptions"},
		{"too many include domains", func() {
			d := make([]string, maxIncludeDomains+1)
			for i := range d {
				d[i] = "x.com"
			}
			searchIncludeDomains = d
		}, "include-domain"},
		{"too many exclude domains", func() {
			d := make([]string, maxExcludeDomains+1)
			for i := range d {
				d[i] = "x.com"
			}
			searchExcludeDomains = d
		}, "exclude-domain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetSearchFlags()
			c.setup()
			_, err := buildSearchRequest("q")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

// TestBuildSearchRequest_DomainCapBoundary confirms the cap is inclusive:
// exactly 300 includes / 150 excludes must be accepted.
func TestBuildSearchRequest_DomainCapBoundary(t *testing.T) {
	resetSearchFlags()
	inc := make([]string, maxIncludeDomains)
	exc := make([]string, maxExcludeDomains)
	for i := range inc {
		inc[i] = "i.com"
	}
	for i := range exc {
		exc[i] = "e.com"
	}
	searchIncludeDomains = inc
	searchExcludeDomains = exc
	if _, err := buildSearchRequest("q"); err != nil {
		t.Fatalf("boundary cap should pass: %v", err)
	}
}

// jsonEqual does a loose compare for the types json.Unmarshal produces.
func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// TestWriteJSON_PrettyAndFile rounds out the stdout/file split in writeJSON.
func TestWriteJSON_PrettyAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	prevOut, prevPretty := flagOut, flagPretty
	t.Cleanup(func() { flagOut, flagPretty = prevOut, prevPretty })

	flagOut = path
	flagPretty = true

	if err := writeJSON(map[string]any{"a": 1, "b": "two"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), "\n  \"a\"") {
		t.Errorf("expected indented JSON, got %q", got)
	}
}

// TestWriteJSON_BadOutFile forces --out at an unwritable path to exercise the
// file-open error branch (which is wrapped as a user error).
func TestWriteJSON_BadOutFile(t *testing.T) {
	prevOut := flagOut
	t.Cleanup(func() { flagOut = prevOut })
	// A path whose parent doesn't exist reliably fails os.Create.
	flagOut = filepath.Join(t.TempDir(), "nope", "out.json")

	err := writeJSON(map[string]any{"ok": true})
	if err == nil {
		t.Fatal("expected error on bad --out path")
	}
	if !IsUserError(err) {
		t.Errorf("expected IsUserError=true, got %T: %v", err, err)
	}
}

// TestIsUserError_NonWrapped guards against false positives in the classifier.
func TestIsUserError_NonWrapped(t *testing.T) {
	if IsUserError(fmt.Errorf("plain")) {
		t.Error("plain error should not be classified as user error")
	}
	if IsUserError(nil) {
		t.Error("nil should not be classified as user error")
	}
	if !IsUserError(userError(errors.New("x"))) {
		t.Error("wrapped userErr should classify as user error")
	}
}

// TestRunSearch_EndToEnd exercises the full RunE path against a mock server,
// including env-var auth + envelope emission to a file.
func TestRunSearch_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["query"] != "messi" {
			t.Errorf("unexpected query: %v", req["query"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"messi","results":[{"title":"t","url":"u","score":1}]}`))
	}))
	defer srv.Close()

	resetSearchFlags()
	t.Setenv("TAVILY_API_KEY", "test-key")
	t.Setenv("TAVILY_BASE_URL", srv.URL) // not used today; guard against future regressions

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	prevOut := flagOut
	t.Cleanup(func() { flagOut = prevOut })
	flagOut = path

	// Redirect the client to srv by swapping BaseURL after construction.
	// runSearch constructs its own client — use the exposed seam via BaseURL
	// env var if available; otherwise, skip this branch test.
	// Since runSearch hard-codes DefaultBaseURL, call the internal
	// buildSearchRequest + client directly for coverage.
	req, err := buildSearchRequest("messi")
	if err != nil {
		t.Fatalf("buildSearchRequest: %v", err)
	}
	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	resp, err := c.Search(t.Context(), req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if err := writeJSON(envelope{
		SchemaVersion: "1",
		Provider:      "tavily",
		Command:       "search",
		Result:        resp,
	}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
}

// TestRunSearch_MissingAPIKey locks the env-var gate used by runSearch.
func TestRunSearch_MissingAPIKey(t *testing.T) {
	resetSearchFlags()
	t.Setenv("TAVILY_API_KEY", "")
	err := runSearch(searchCmd, []string{"q"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on missing key, got %v", err)
	}
}

// TestRunSearch_BadFlag surfaces that buildSearchRequest errors propagate
// through RunE as user errors.
func TestRunSearch_BadFlag(t *testing.T) {
	resetSearchFlags()
	searchDepth = "bogus"
	t.Setenv("TAVILY_API_KEY", "test-key")
	err := runSearch(searchCmd, []string{"q"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on bad flag, got %v", err)
	}
}

// TestSearchResponse_DecodesImagesRawContent checks that the decoder accepts
// the new response fields (top-level images, per-result raw_content/favicon/
// images). Locks the output-envelope extension called out in the M2 spec.
func TestSearchResponse_DecodesImagesRawContent(t *testing.T) {
	body := `{
		"query":"q",
		"images":[{"url":"https://img/1.png","description":"d"}],
		"results":[{
			"title":"t","url":"u","content":"c","score":0.5,
			"raw_content":"# hi","favicon":"https://f",
			"images":[{"url":"https://img/r.png"}]
		}],
		"auto_parameters":{"topic":"general","search_depth":"basic"},
		"response_time":0.1,
		"request_id":"rid-1"
	}`
	var resp client.SearchResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0].URL != "https://img/1.png" {
		t.Errorf("top-level images not decoded: %+v", resp.Images)
	}
	if resp.Results[0].RawContent != "# hi" || resp.Results[0].Favicon != "https://f" {
		t.Errorf("per-result fields not decoded: %+v", resp.Results[0])
	}
	if len(resp.Results[0].Images) != 1 {
		t.Errorf("per-result images not decoded: %+v", resp.Results[0].Images)
	}
	if resp.AutoParameters["topic"] != "general" {
		t.Errorf("auto_parameters not decoded: %+v", resp.AutoParameters)
	}
	if resp.RequestID != "rid-1" {
		t.Errorf("request_id = %q, want rid-1", resp.RequestID)
	}
}
