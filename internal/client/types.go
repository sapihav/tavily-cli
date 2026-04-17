// Package client contains the HTTP client and types for the Tavily API.
package client

// SearchRequest is the JSON body sent to POST /search.
//
// Fields are limited to what Milestone 1 supports; extra fields can be added
// incrementally in later milestones (topic variants, include_images, etc.).
type SearchRequest struct {
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results,omitempty"`
	SearchDepth   string `json:"search_depth,omitempty"`
	Topic         string `json:"topic,omitempty"`
	IncludeAnswer bool   `json:"include_answer,omitempty"`
	// APIKey is only populated on the body-auth fallback path. It is never
	// populated when Authorization: Bearer is used successfully. The client
	// strips it from request logs/verbose output.
	APIKey string `json:"api_key,omitempty"`
}

// SearchResult is a single hit in SearchResponse.Results.
type SearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score"`
	PublishedDate string  `json:"published_date,omitempty"`
}

// SearchResponse mirrors the Tavily /search response shape.
type SearchResponse struct {
	Query        string         `json:"query"`
	Answer       string         `json:"answer,omitempty"`
	Results      []SearchResult `json:"results"`
	ResponseTime float64        `json:"response_time,omitempty"`
}
