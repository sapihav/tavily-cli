package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the Tavily API base. Override via Client.BaseURL for tests.
const DefaultBaseURL = "https://api.tavily.com"

// Exit code classes returned from the CLI; exposed so cmd/ can map errors.
const (
	ExitAPIError     = 1
	ExitUserError    = 2
	ExitNetworkError = 3
)

// ErrMissingAPIKey signals that TAVILY_API_KEY is not set. CLI maps to exit 2.
var ErrMissingAPIKey = errors.New("TAVILY_API_KEY not set — get one at https://app.tavily.com")

// APIError is returned when the server responds with HTTP >= 400 after retries
// are exhausted. Maps to exit code 1.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tavily API error: HTTP %d: %s", e.StatusCode, e.Body)
}

// NetworkError wraps transport-level failures (DNS, TCP, TLS, context deadline).
// Maps to exit code 3.
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string { return fmt.Sprintf("network error: %v", e.Err) }
func (e *NetworkError) Unwrap() error { return e.Err }

// Client is a thin wrapper over net/http with retry + backoff on 429/5xx.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTP       *http.Client
	MaxRetries int
	// BackoffBase is the base sleep for exponential backoff (attempt 1 = base,
	// attempt 2 = 2*base, ...). Exposed as a field so tests can shrink it.
	BackoffBase time.Duration
	// UseBodyAuth forces api_key in body instead of Authorization header. The
	// client flips this automatically on a 401 from the header path.
	UseBodyAuth bool
}

// New builds a Client with sensible defaults. apiKey must be non-empty; callers
// (the CLI) are responsible for reading the env var and returning
// ErrMissingAPIKey when absent.
func New(apiKey string, maxRetries int) *Client {
	return &Client{
		APIKey:      apiKey,
		BaseURL:     DefaultBaseURL,
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		MaxRetries:  maxRetries,
		BackoffBase: 500 * time.Millisecond,
	}
}

// Search calls POST /search and returns the decoded response.
//
// Retry policy: up to MaxRetries additional attempts on 429 / 5xx / transport
// error, with exponential backoff (BackoffBase * 2^(attempt-1)). 4xx other than
// 429 fails fast — no point retrying a bad request.
//
// Auth: prefers Authorization: Bearer. On 401 it flips to body api_key and
// retries once (still within the retry budget).
func (c *Client) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if c.APIKey == "" {
		return nil, ErrMissingAPIKey
	}

	var lastErr error
	attempts := c.MaxRetries + 1 // +1 for the initial attempt

	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := c.doSearch(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// 401 on header-based auth: one-shot fallback to body auth, does not
		// consume the retry budget other than this attempt.
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized && !c.UseBodyAuth {
			c.UseBodyAuth = true
			continue
		}

		if !isRetryable(err) {
			return nil, err
		}

		if attempt == attempts {
			break
		}

		// Exponential backoff; respect ctx cancellation.
		sleep := c.BackoffBase * time.Duration(1<<(attempt-1))
		select {
		case <-ctx.Done():
			return nil, &NetworkError{Err: ctx.Err()}
		case <-time.After(sleep):
		}
	}

	return nil, lastErr
}

// doSearch performs a single HTTP round-trip. All retry / backoff decisions are
// made by the caller (Search).
func (c *Client) doSearch(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	body := req
	if c.UseBodyAuth {
		body.APIKey = c.APIKey
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/search", bytes.NewReader(buf))
	if err != nil {
		return nil, &NetworkError{Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if !c.UseBodyAuth {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpResp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, &NetworkError{Err: err}
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &NetworkError{Err: err}
	}

	if httpResp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: httpResp.StatusCode, Body: string(respBytes)}
	}

	var out SearchResponse
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// isRetryable reports whether err warrants another attempt.
func isRetryable(err error) bool {
	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	return false
}
