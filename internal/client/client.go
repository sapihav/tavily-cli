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
	var out SearchResponse
	err := c.post(ctx, "/search", &req, &req.APIKey, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Extract calls POST /extract with a batch of URLs and returns the decoded
// response. Uses the same retry / auth-fallback policy as Search.
func (c *Client) Extract(ctx context.Context, req ExtractRequest) (*ExtractResponse, error) {
	var out ExtractResponse
	err := c.post(ctx, "/extract", &req, &req.APIKey, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// post runs a POST <path> round-trip with retry, backoff, and 401→body-auth
// fallback. apiKeyField must point at the request struct's api_key field so
// body-auth mode can populate it without this helper knowing the request type.
func (c *Client) post(ctx context.Context, path string, req any, apiKeyField *string, out any) error {
	if c.APIKey == "" {
		return ErrMissingAPIKey
	}

	var lastErr error
	attempts := c.MaxRetries + 1

	for attempt := 1; attempt <= attempts; attempt++ {
		if c.UseBodyAuth {
			*apiKeyField = c.APIKey
		} else {
			*apiKeyField = ""
		}

		err := c.roundTrip(ctx, path, req, out)
		if err == nil {
			return nil
		}
		lastErr = err

		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized && !c.UseBodyAuth {
			c.UseBodyAuth = true
			continue
		}

		if !isRetryable(err) {
			return err
		}
		if attempt == attempts {
			break
		}

		sleep := c.BackoffBase * time.Duration(1<<(attempt-1))
		select {
		case <-ctx.Done():
			return &NetworkError{Err: ctx.Err()}
		case <-time.After(sleep):
		}
	}
	return lastErr
}

// roundTrip performs a single HTTP request/response cycle.
func (c *Client) roundTrip(ctx context.Context, path string, req any, out any) error {
	buf, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return &NetworkError{Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if !c.UseBodyAuth {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpResp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return &NetworkError{Err: err}
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return &NetworkError{Err: err}
	}

	if httpResp.StatusCode >= 400 {
		return &APIError{StatusCode: httpResp.StatusCode, Body: string(respBytes)}
	}

	if err := json.Unmarshal(respBytes, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
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
