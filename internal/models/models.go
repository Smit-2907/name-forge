package models

import "time"

// GenerateRequest represents the body of the POST /generate request
type GenerateRequest struct {
	Description string   `json:"description"`
	Style       []string `json:"style"`
	Themes      []string `json:"themes"`
	TLDs        []string `json:"tlds"`
	Avoid       []string `json:"avoid"`
}

type ProviderOffer struct {
	Platform string  `json:"platform"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	IsBest   bool    `json:"is_best"`
}

// ResultItem represents a single generated name result with its domain availability and scoring
type ResultItem struct {
	Name      string          `json:"name"`
	Domain    string          `json:"domain"`
	Available bool            `json:"available"`
	Price     float64         `json:"price"`
	Currency  string          `json:"currency"`
	Platform  string          `json:"platform"`
	Score     int             `json:"score"`
	Offers    []ProviderOffer `json:"offers,omitempty"`
}

// GenerateResponse is the response body returned by the API
type GenerateResponse struct {
	Results []ResultItem `json:"results"`
}

// Search represents a row in the searches table
type Search struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Style       []string  `json:"style"`
	Themes      []string  `json:"themes"`
	TLDs        []string  `json:"tlds"`
	Avoid       []string  `json:"avoid"`
	CreatedAt   time.Time `json:"created_at"`
}

// GeneratedName represents a generated name suggestion before/after checks
type GeneratedName struct {
	ID            int64     `json:"id"`
	SearchID      int64     `json:"search_id"`
	Name          string    `json:"name"`
	GeneratorType string    `json:"generator_type"`
	Score         int       `json:"score"`
	CreatedAt     time.Time `json:"created_at"`
}

// DomainCheck represents the check logs for specific TLDs checked
type DomainCheck struct {
	ID        int64           `json:"id"`
	NameID    int64           `json:"name_id"`
	Domain    string          `json:"domain"`
	TLD       string          `json:"tld"`
	Available bool            `json:"available"`
	Price     float64         `json:"price"`
	Currency  string          `json:"currency"`
	Platform  string          `json:"platform"`
	Offers    []ProviderOffer `json:"offers,omitempty"`
	CheckedAt time.Time       `json:"checked_at"`
}

// AnalyticsSummary provides metrics on searches, domains, and check latencies
type AnalyticsSummary struct {
	TotalSearches      int64   `json:"total_searches"`
	TotalNamesGen      int64   `json:"total_names_generated"`
	TotalDomainChecks  int64   `json:"total_domain_checks"`
	AvailabilityRate   float64 `json:"availability_rate"`
	AverageSearchSpeed float64 `json:"average_search_speed_ms"`
}
