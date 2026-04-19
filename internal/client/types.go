// Package client contains the HTTP client and types for the Tavily API.
package client

// SearchRequest is the JSON body sent to POST /search.
//
// Fields match the Tavily /search OpenAPI schema. All optional fields are
// omitempty so the server applies its documented defaults.
//
// IncludeAnswer and IncludeRawContent are `any` because the upstream API
// accepts either a boolean OR a string enum (`basic|advanced` and
// `markdown|text` respectively). The CLI normalises bare-flag usage to the
// `basic` / `markdown` string variants before encoding.
type SearchRequest struct {
	Query                    string   `json:"query"`
	MaxResults               int      `json:"max_results,omitempty"`
	SearchDepth              string   `json:"search_depth,omitempty"`
	Topic                    string   `json:"topic,omitempty"`
	TimeRange                string   `json:"time_range,omitempty"`
	StartDate                string   `json:"start_date,omitempty"`
	EndDate                  string   `json:"end_date,omitempty"`
	IncludeDomains           []string `json:"include_domains,omitempty"`
	ExcludeDomains           []string `json:"exclude_domains,omitempty"`
	Country                  string   `json:"country,omitempty"`
	IncludeImages            bool     `json:"include_images,omitempty"`
	IncludeImageDescriptions bool     `json:"include_image_descriptions,omitempty"`
	IncludeAnswer            any      `json:"include_answer,omitempty"`
	IncludeRawContent        any      `json:"include_raw_content,omitempty"`
	IncludeFavicon           bool     `json:"include_favicon,omitempty"`
	ChunksPerSource          int      `json:"chunks_per_source,omitempty"`
	AutoParameters           bool     `json:"auto_parameters,omitempty"`
	ExactMatch               bool     `json:"exact_match,omitempty"`
	SafeSearch               bool     `json:"safe_search,omitempty"`
	// APIKey is only populated on the body-auth fallback path. It is never
	// populated when Authorization: Bearer is used successfully. The client
	// strips it from request logs/verbose output.
	APIKey string `json:"api_key,omitempty"`
}

// SearchImage is an image entry returned by /search. `description` is only
// populated when include_image_descriptions=true on the request.
type SearchImage struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// SearchResult is a single hit in SearchResponse.Results.
type SearchResult struct {
	Title         string        `json:"title"`
	URL           string        `json:"url"`
	Content       string        `json:"content"`
	Score         float64       `json:"score"`
	PublishedDate string        `json:"published_date,omitempty"`
	RawContent    string        `json:"raw_content,omitempty"`
	Favicon       string        `json:"favicon,omitempty"`
	Images        []SearchImage `json:"images,omitempty"`
}

// SearchResponse mirrors the Tavily /search response shape.
type SearchResponse struct {
	Query          string         `json:"query"`
	Answer         string         `json:"answer,omitempty"`
	Images         []SearchImage  `json:"images,omitempty"`
	Results        []SearchResult `json:"results"`
	AutoParameters map[string]any `json:"auto_parameters,omitempty"`
	ResponseTime   float64        `json:"response_time,omitempty"`
	RequestID      string         `json:"request_id,omitempty"`
}
