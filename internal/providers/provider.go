package providers

import "context"

type DomainResult struct {
	Domain    string `json:"domain"`
	Available bool   `json:"available"`
}

type PricePlan struct {
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
}

type PriceResult struct {
	Price    float64     `json:"price"`
	Currency string      `json:"currency"`
	Platform string      `json:"platform"`
	Plans    []PricePlan `json:"plans,omitempty"`
}

type DomainProvider interface {
	CheckAvailability(ctx context.Context, domain string) (*DomainResult, error)
	GetPrice(ctx context.Context, domain string) (*PriceResult, error)
}
