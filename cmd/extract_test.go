package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sapihav/tavily-cli/internal/client"
)

// resetExtractFlags zeroes every package-level extract flag so each test runs
// against a clean baseline (cobra binds to globals).
func resetExtractFlags() {
	extractURLs = ""
	extractQuery = ""
	extractDepth = "basic"
	extractFormat = "markdown"
	extractChunksPerSource = 0
	extractIncludeImages = false
	extractIncludeFavicon = false
	extractIncludeUsage = false
	extractTimeoutSec = 60
}

func TestCollectExtractURLs_ArgsDedup(t *testing.T) {
	resetExtractFlags()
	got, err := collectExtractURLs(
		[]string{"https://a.example", "https://b.example", "https://a.example", "  ", ""},
		"",
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatalf("collectExtractURLs: %v", err)
	}
	want := []string{"https://a.example", "https://b.example"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectExtractURLs_FlagCSV(t *testing.T) {
	got, err := collectExtractURLs(nil, "https://a, https://b ,https://a", strings.NewReader(""))
	if err != nil {
		t.Fatalf("collectExtractURLs: %v", err)
	}
	want := []string{"https://a", "https://b"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectExtractURLs_StdinDash(t *testing.T) {
	stdin := strings.NewReader("https://s1\nhttps://s2\n\nhttps://s1\n")
	got, err := collectExtractURLs([]string{"-", "https://arg"}, "", stdin)
	if err != nil {
		t.Fatalf("collectExtractURLs: %v", err)
	}
	want := []string{"https://arg", "https://s1", "https://s2"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectExtractURLs_MixedSources(t *testing.T) {
	stdin := strings.NewReader("https://from-stdin\n")
	got, err := collectExtractURLs([]string{"https://arg", "-"}, "https://csv1,https://csv2", stdin)
	if err != nil {
		t.Fatalf("collectExtractURLs: %v", err)
	}
	// Args first, then CSV, then stdin — preserving first-seen order.
	want := []string{"https://arg", "https://csv1", "https://csv2", "https://from-stdin"}
	if !stringSliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildExtractRequest_Defaults(t *testing.T) {
	resetExtractFlags()
	req, err := buildExtractRequest([]string{"https://a"})
	if err != nil {
		t.Fatalf("buildExtractRequest: %v", err)
	}
	if len(req.URLs) != 1 || req.URLs[0] != "https://a" {
		t.Errorf("URLs = %v", req.URLs)
	}
	if req.ExtractDepth != "basic" || req.Format != "markdown" {
		t.Errorf("unexpected defaults: %+v", req)
	}
	if req.IncludeImages || req.IncludeFavicon || req.IncludeUsage {
		t.Errorf("bool defaults should be false: %+v", req)
	}
}

// Golden request replay: every flag exercised, JSON body pinned field-by-field.
func TestBuildExtractRequest_AllFlags(t *testing.T) {
	resetExtractFlags()
	extractQuery = "quarterly revenue"
	extractDepth = "advanced"
	extractFormat = "text"
	extractChunksPerSource = 5
	extractIncludeImages = true
	extractIncludeFavicon = true
	extractIncludeUsage = true

	req, err := buildExtractRequest([]string{"https://a", "https://b"})
	if err != nil {
		t.Fatalf("buildExtractRequest: %v", err)
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
		"urls":              []any{"https://a", "https://b"},
		"query":             "quarterly revenue",
		"extract_depth":     "advanced",
		"format":            "text",
		"chunks_per_source": float64(5),
		"include_images":    true,
		"include_favicon":   true,
		"include_usage":     true,
	}
	if len(parsed) != len(want) {
		t.Errorf("field count mismatch: got %d, want %d\n  got:  %v\n  want: %v",
			len(parsed), len(want), parsed, want)
	}
	for k, v := range want {
		ab, _ := json.Marshal(parsed[k])
		bb, _ := json.Marshal(v)
		if string(ab) != string(bb) {
			t.Errorf("%s: got %s, want %s", k, ab, bb)
		}
	}
	if strings.Contains(string(got), "api_key") {
		t.Errorf("api_key must not appear in wire payload: %s", got)
	}
}

func TestBuildExtractRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"bad depth", func() { extractDepth = "ludicrous" }, "extract-depth"},
		{"bad format", func() { extractFormat = "pdf" }, "format"},
		{"chunks too low", func() { extractChunksPerSource = -1 }, "chunks-per-source"},
		{"chunks too high", func() { extractChunksPerSource = 6 }, "chunks-per-source"},
		{"bad timeout", func() { extractTimeoutSec = 0 }, "timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetExtractFlags()
			c.setup()
			_, err := buildExtractRequest([]string{"https://a"})
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q does not mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

func TestBuildExtractRequest_ChunksBoundaries(t *testing.T) {
	for _, v := range []int{1, 5} {
		resetExtractFlags()
		extractChunksPerSource = v
		if _, err := buildExtractRequest([]string{"https://a"}); err != nil {
			t.Errorf("chunks=%d should be valid: %v", v, err)
		}
	}
}

// Wire-format round-trip against a mock server: request body + response decode.
func TestExtract_MockServer_Golden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/extract" {
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
		if urls, _ := parsed["urls"].([]any); len(urls) != 2 {
			t.Errorf("urls len = %d, want 2", len(urls))
		}
		if parsed["extract_depth"] != "advanced" {
			t.Errorf("extract_depth = %v", parsed["extract_depth"])
		}
		if parsed["format"] != "text" {
			t.Errorf("format = %v", parsed["format"])
		}
		if parsed["include_usage"] != true {
			t.Errorf("include_usage = %v", parsed["include_usage"])
		}
		if _, hasKey := parsed["api_key"]; hasKey {
			t.Errorf("api_key should not appear on Bearer path")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results":[
				{"url":"https://a","raw_content":"# A","images":["https://i/a.png"],"favicon":"https://a/favicon.ico"}
			],
			"failed_results":[{"url":"https://b","error":"timeout"}],
			"response_time":0.25,
			"request_id":"rid-ext",
			"usage":{"credits":2}
		}`))
	}))
	defer srv.Close()

	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	resp, err := c.Extract(context.Background(), client.ExtractRequest{
		URLs:            []string{"https://a", "https://b"},
		ExtractDepth:    "advanced",
		Format:          "text",
		ChunksPerSource: 3,
		IncludeImages:   true,
		IncludeFavicon:  true,
		IncludeUsage:    true,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "https://a" {
		t.Errorf("results: %+v", resp.Results)
	}
	if resp.Results[0].RawContent != "# A" || resp.Results[0].Favicon != "https://a/favicon.ico" {
		t.Errorf("per-result fields: %+v", resp.Results[0])
	}
	if len(resp.Results[0].Images) != 1 {
		t.Errorf("images: %+v", resp.Results[0].Images)
	}
	if len(resp.FailedResults) != 1 || resp.FailedResults[0].URL != "https://b" {
		t.Errorf("failed: %+v", resp.FailedResults)
	}
	if resp.RequestID != "rid-ext" {
		t.Errorf("request_id = %q", resp.RequestID)
	}
	if v, _ := resp.Usage["credits"].(float64); v != 2 {
		t.Errorf("usage: %+v", resp.Usage)
	}
}

func TestExtract_401FallsBackToBodyAuth(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		hasHeader := r.Header.Get("Authorization") != ""
		hasBodyKey := strings.Contains(string(body), `"api_key"`)
		if n == 1 {
			if !hasHeader || hasBodyKey {
				t.Errorf("first call should use header auth only")
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`unauthorized`))
			return
		}
		if hasHeader || !hasBodyKey {
			t.Errorf("second call should use body auth only")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := client.New("test-key", 2)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	if _, err := c.Extract(context.Background(), client.ExtractRequest{URLs: []string{"https://a"}}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !c.UseBodyAuth {
		t.Error("expected UseBodyAuth=true after 401 fallback")
	}
}

func TestExtract_429RetriesThen200(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := client.New("test-key", 3)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	if _, err := c.Extract(context.Background(), client.ExtractRequest{URLs: []string{"https://a"}}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestExtract_500Exhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	c := client.New("test-key", 1)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	_, err := c.Extract(context.Background(), client.ExtractRequest{URLs: []string{"https://a"}})
	var apiErr *client.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Fatalf("expected APIError 500, got %v", err)
	}
}

func TestExtract_Timeout(t *testing.T) {
	// Handler observes context cancellation + returns promptly so srv.Close()
	// doesn't hang. A small sleep guarantees the client's 20ms deadline fires
	// first; select on r.Context().Done() ensures we don't wait the full sleep
	// once the client disconnects.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer srv.Close()

	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.Extract(ctx, client.ExtractRequest{URLs: []string{"https://a"}})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var netErr *client.NetworkError
	if !asNetworkError(err, &netErr) {
		t.Fatalf("expected NetworkError, got %T: %v", err, err)
	}
}

func TestRunExtract_MissingAPIKey(t *testing.T) {
	resetExtractFlags()
	t.Setenv("TAVILY_API_KEY", "")
	err := runExtract(extractCmd, []string{"https://a"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on missing key, got %v", err)
	}
}

func TestRunExtract_NoURLs(t *testing.T) {
	resetExtractFlags()
	t.Setenv("TAVILY_API_KEY", "k")
	// Pipe an empty reader via cmd.SetIn so "-" doesn't block.
	extractCmd.SetIn(strings.NewReader(""))
	err := runExtract(extractCmd, nil)
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no URLs") {
		t.Errorf("error should mention no URLs: %v", err)
	}
}

func TestRunExtract_BadFlag(t *testing.T) {
	resetExtractFlags()
	extractDepth = "bogus"
	t.Setenv("TAVILY_API_KEY", "k")
	err := runExtract(extractCmd, []string{"https://a"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error, got %v", err)
	}
}

// --- small test helpers ---

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// asAPIError and asNetworkError avoid importing errors just for As() here.
func asAPIError(err error, target **client.APIError) bool {
	for e := err; e != nil; {
		if ae, ok := e.(*client.APIError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

func asNetworkError(err error, target **client.NetworkError) bool {
	for e := err; e != nil; {
		if ne, ok := e.(*client.NetworkError); ok {
			*target = ne
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
