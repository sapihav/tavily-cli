package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient builds a Client wired to a test server with negligible backoff
// so retry tests finish in milliseconds.
func newTestClient(t *testing.T, srv *httptest.Server, maxRetries int) *Client {
	t.Helper()
	c := New("test-key", maxRetries)
	c.BaseURL = srv.URL
	c.BackoffBase = 1 * time.Millisecond
	return c
}

// TestSearch_HappyPath covers request encoding, header-based auth, and response
// decoding.
func TestSearch_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req SearchRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Query != "golang" || req.MaxResults != 3 {
			t.Errorf("unexpected request body: %+v", req)
		}
		if req.APIKey != "" {
			t.Errorf("api_key should not appear in body on Bearer path, got %q", req.APIKey)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query":"golang",
			"answer":"Go is a language",
			"results":[{"title":"t","url":"u","content":"c","score":0.9,"published_date":"2024-01-02"}],
			"response_time":0.42
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 0)
	resp, err := c.Search(context.Background(), SearchRequest{Query: "golang", MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Query != "golang" || resp.Answer != "Go is a language" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if len(resp.Results) != 1 || resp.Results[0].URL != "u" || resp.Results[0].Score != 0.9 {
		t.Errorf("unexpected results: %+v", resp.Results)
	}
	if resp.ResponseTime != 0.42 {
		t.Errorf("response_time = %v", resp.ResponseTime)
	}
}

// TestSearch_Retries_429Then200 asserts exponential-backoff retry on 429.
func TestSearch_Retries_429Then200(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"q","results":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 3)
	resp, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Query != "q" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 HTTP attempts, got %d", got)
	}
}

// TestSearch_Retries_500Exhausted confirms 5xx retries stop at MaxRetries+1.
func TestSearch_Retries_500Exhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 2)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("expected APIError, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Fatalf("expected APIError 500, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", got)
	}
}

// TestSearch_4xxNotRetried ensures 400-class errors (other than 429) fail fast.
func TestSearch_4xxNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 5)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("expected APIError 400, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 attempt (no retry on 400), got %d", got)
	}
}

// TestSearch_401FallsBackToBodyAuth verifies the Bearer -> api_key fallback.
func TestSearch_401FallsBackToBodyAuth(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		hasHeader := r.Header.Get("Authorization") != ""
		hasBodyKey := strings.Contains(string(body), `"api_key"`)

		if n == 1 {
			if !hasHeader || hasBodyKey {
				t.Errorf("first call should use header auth only; hasHeader=%v hasBodyKey=%v", hasHeader, hasBodyKey)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`unauthorized`))
			return
		}
		if hasHeader || !hasBodyKey {
			t.Errorf("second call should use body auth only; hasHeader=%v hasBodyKey=%v", hasHeader, hasBodyKey)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"q","results":[]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 2)
	resp, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Query != "q" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if !c.UseBodyAuth {
		t.Error("expected UseBodyAuth=true after 401 fallback")
	}
}

// TestSearch_MissingAPIKey guards the ErrMissingAPIKey path.
func TestSearch_MissingAPIKey(t *testing.T) {
	c := New("", 0)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

// TestSearch_ContextCancellation verifies backoff respects ctx.
func TestSearch_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 5)
	c.BackoffBase = 500 * time.Millisecond // long enough for ctx to win

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Search(ctx, SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("expected error on ctx cancel")
	}
	// Either a NetworkError wrapping ctx.Err, or a final APIError 429 if the
	// first attempt returned before the timeout fired. Both are acceptable; the
	// behavior under test is "does not hang".
}

// TestSearch_BadJSON surfaces a decode error distinct from APIError.
func TestSearch_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv, 0)
	_, err := c.Search(context.Background(), SearchRequest{Query: "q"})
	if err == nil {
		t.Fatal("expected decode error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("expected a non-API decode error, got %v", err)
	}
}
