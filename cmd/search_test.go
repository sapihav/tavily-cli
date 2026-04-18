package cmd

import (
	"encoding/json"
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
