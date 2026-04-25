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

// resetMapFlags zeroes every package-level map flag so each test starts from
// a known baseline.
func resetMapFlags() {
	mapMaxDepth = 0
	mapMaxBreadth = 0
	mapLimit = 0
	mapInstructions = ""
	mapSelectPaths = nil
	mapExcludePaths = nil
	mapSelectDomains = nil
	mapExcludeDomains = nil
	mapAllowExternal = true
	mapDryRun = false
	mapTimeoutSec = 60
}

func TestResolveMapURL_Positional(t *testing.T) {
	got, err := resolveMapURL("https://example.com", strings.NewReader(""))
	if err != nil || got != "https://example.com" {
		t.Fatalf("resolveMapURL: got=%q err=%v", got, err)
	}
}

func TestResolveMapURL_StdinDash(t *testing.T) {
	got, err := resolveMapURL("-", strings.NewReader("\n  \nhttps://from-stdin\nignored\n"))
	if err != nil {
		t.Fatalf("resolveMapURL: %v", err)
	}
	if got != "https://from-stdin" {
		t.Errorf("got %q", got)
	}
}

func TestResolveMapURL_StdinEmpty(t *testing.T) {
	_, err := resolveMapURL("-", strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error on empty stdin")
	}
}

func TestBuildMapRequest_Defaults(t *testing.T) {
	resetMapFlags()
	req, err := buildMapRequest("https://x", false)
	if err != nil {
		t.Fatalf("buildMapRequest: %v", err)
	}
	if req.URL != "https://x" {
		t.Errorf("URL = %q", req.URL)
	}
	// allow_external is omitted from the wire format unless the user explicitly
	// set the flag — this preserves the server default (true).
	if req.AllowExternal != nil {
		t.Errorf("AllowExternal should be nil when flag not set, got %v", *req.AllowExternal)
	}
}

// Wire-format pin: every flag set, JSON body checked field-by-field.
func TestBuildMapRequest_AllFlags(t *testing.T) {
	resetMapFlags()
	mapMaxDepth = 3
	mapMaxBreadth = 50
	mapLimit = 200
	mapInstructions = "focus on api docs"
	mapSelectPaths = []string{`^/docs/.*`}
	mapExcludePaths = []string{`^/admin/.*`, `^/private/.*`}
	mapSelectDomains = []string{`^docs\.example\.com$`}
	mapExcludeDomains = []string{`^private\.example\.com$`}
	mapAllowExternal = false

	req, err := buildMapRequest("https://example.com", true)
	if err != nil {
		t.Fatalf("buildMapRequest: %v", err)
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
		"url":             "https://example.com",
		"max_depth":       float64(3),
		"max_breadth":     float64(50),
		"limit":           float64(200),
		"instructions":    "focus on api docs",
		"select_paths":    []any{"^/docs/.*"},
		"exclude_paths":   []any{"^/admin/.*", "^/private/.*"},
		"select_domains":  []any{`^docs\.example\.com$`},
		"exclude_domains": []any{`^private\.example\.com$`},
		"allow_external":  false,
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

func TestBuildMapRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantSub string
	}{
		{"empty url", func() {}, "url"},
		{"depth too high", func() { mapMaxDepth = 6 }, "max-depth"},
		{"depth negative", func() { mapMaxDepth = -1 }, "max-depth"},
		{"breadth too high", func() { mapMaxBreadth = 501 }, "max-breadth"},
		{"limit negative", func() { mapLimit = -5 }, "limit"},
		{"bad timeout", func() { mapTimeoutSec = 0 }, "timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetMapFlags()
			c.setup()
			url := "https://x"
			if c.name == "empty url" {
				url = ""
			}
			_, err := buildMapRequest(url, false)
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
func TestMap_MockServer_Golden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/map" {
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
		if parsed["max_depth"] != float64(3) {
			t.Errorf("max_depth = %v", parsed["max_depth"])
		}
		if _, hasKey := parsed["api_key"]; hasKey {
			t.Errorf("api_key should not appear on Bearer path")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"base_url":"example.com",
			"results":["https://example.com/a","https://example.com/b"],
			"response_time":1.23,
			"request_id":"rid-map"
		}`))
	}))
	defer srv.Close()

	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	resp, err := c.Map(context.Background(), client.MapRequest{
		URL:      "https://example.com",
		MaxDepth: 3,
	})
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if resp.BaseURL != "example.com" {
		t.Errorf("base_url = %q", resp.BaseURL)
	}
	if len(resp.Results) != 2 || resp.Results[0] != "https://example.com/a" {
		t.Errorf("results: %+v", resp.Results)
	}
	if resp.ResponseTime != 1.23 || resp.RequestID != "rid-map" {
		t.Errorf("metadata: %+v", resp)
	}
}

func TestMap_4xxNotRetried(t *testing.T) {
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

	_, err := c.Map(context.Background(), client.MapRequest{URL: "https://x"})
	var apiErr *client.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("expected APIError 400, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 4xx)", calls.Load())
	}
}

func TestMap_500Exhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.New("test-key", 1)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	_, err := c.Map(context.Background(), client.MapRequest{URL: "https://x"})
	var apiErr *client.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Fatalf("expected APIError 500, got %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("attempts = %d, want 2 (1 + 1 retry)", calls.Load())
	}
}

func TestMap_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := client.New("test-key", 0)
	c.BaseURL = srv.URL
	c.BackoffBase = time.Millisecond

	_, err := c.Map(context.Background(), client.MapRequest{URL: "https://x"})
	if err == nil {
		t.Fatal("expected decode error")
	}
	var apiErr *client.APIError
	if asAPIError(err, &apiErr) {
		t.Fatalf("expected non-API decode error, got %v", err)
	}
}

func TestMap_TransportError(t *testing.T) {
	c := client.New("test-key", 0)
	// Unreachable host port — TCP refused.
	c.BaseURL = "http://127.0.0.1:1"
	c.BackoffBase = time.Millisecond

	_, err := c.Map(context.Background(), client.MapRequest{URL: "https://x"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	var netErr *client.NetworkError
	if !asNetworkError(err, &netErr) {
		t.Fatalf("expected NetworkError, got %T: %v", err, err)
	}
}

func TestRunMap_RequiresArg(t *testing.T) {
	resetMapFlags()
	// cobra.ExactArgs(1) check.
	if err := mapCmd.Args(mapCmd, nil); err == nil {
		t.Errorf("expected ExactArgs(1) to reject 0 args")
	}
	if err := mapCmd.Args(mapCmd, []string{"a", "b"}); err == nil {
		t.Errorf("expected ExactArgs(1) to reject 2 args")
	}
	if err := mapCmd.Args(mapCmd, []string{"https://x"}); err != nil {
		t.Errorf("ExactArgs(1) rejected 1 arg: %v", err)
	}
}

func TestRunMap_MissingAPIKey(t *testing.T) {
	resetMapFlags()
	t.Setenv("TAVILY_API_KEY", "")
	err := runMap(mapCmd, []string{"https://x"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error on missing key, got %v", err)
	}
}

func TestRunMap_BadFlag(t *testing.T) {
	resetMapFlags()
	mapMaxDepth = 99
	t.Setenv("TAVILY_API_KEY", "k")
	err := runMap(mapCmd, []string{"https://x"})
	if err == nil || !IsUserError(err) {
		t.Fatalf("expected user error, got %v", err)
	}
}

func TestRunMap_DryRunNoNetwork(t *testing.T) {
	resetMapFlags()
	mapDryRun = true
	mapMaxDepth = 2
	mapInstructions = "secret-instr"
	t.Setenv("TAVILY_API_KEY", "should-not-leak")

	// Capture stdout.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	err := runMap(mapCmd, []string{"https://x"})
	if err != nil {
		t.Fatalf("runMap dry-run: %v", err)
	}
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()

	var env envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v\nstdout=%s", err, out)
	}
	if env.Command != "map" {
		t.Errorf("command = %q", env.Command)
	}
	resMap, ok := env.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", env.Result)
	}
	if resMap["dry_run"] != true {
		t.Errorf("dry_run flag missing in output: %v", resMap)
	}
	if resMap["endpoint"] != "POST /map" {
		t.Errorf("endpoint = %v", resMap["endpoint"])
	}
	if strings.Contains(out, "should-not-leak") {
		t.Errorf("api key leaked into dry-run output: %s", out)
	}
}

func TestRunMap_StdinURL(t *testing.T) {
	resetMapFlags()
	mapDryRun = true // avoid network
	t.Setenv("TAVILY_API_KEY", "k")
	mapCmd.SetIn(strings.NewReader("https://from-stdin\n"))

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig; mapCmd.SetIn(nil) }()

	if err := runMap(mapCmd, []string{"-"}); err != nil {
		t.Fatalf("runMap: %v", err)
	}
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if !strings.Contains(buf.String(), "https://from-stdin") {
		t.Errorf("stdin URL not echoed in dry-run: %s", buf.String())
	}
}

func TestRunMap_RepeatedFlags(t *testing.T) {
	resetMapFlags()
	mapDryRun = true
	mapSelectPaths = []string{"^/a", "^/b"}
	mapExcludeDomains = []string{`^x\.com$`, `^y\.com$`}
	t.Setenv("TAVILY_API_KEY", "k")

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	if err := runMap(mapCmd, []string{"https://x"}); err != nil {
		t.Fatalf("runMap: %v", err)
	}
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	for _, sub := range []string{`"^/a"`, `"^/b"`, `"^x\\.com$"`, `"^y\\.com$"`} {
		if !strings.Contains(out, sub) {
			t.Errorf("dry-run output missing %s\n  full=%s", sub, out)
		}
	}
}

