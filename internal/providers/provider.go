package providers

import "context"

type DomainResult struct {
	Domain    string `json:"domain"`
	Available bool   `json:"available"`
}

type PriceResult struct {
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	Platform string  `json:"platform"`
}

type DomainProvider interface {
	CheckAvailability(ctx context.Context, domain string) (*DomainResult, error)
	GetPrice(ctx context.Context, domain string) (*PriceResult, error)
}
