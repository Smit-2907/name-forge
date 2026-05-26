package providers

import (
	"context"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type HostingerProvider struct {
	APIKey string
	Client *http.Client
}

func NewHostingerProvider(apiKey string) *HostingerProvider {
	return &HostingerProvider{
		APIKey: apiKey,
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (h *HostingerProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	// Always mock for now, or use API if key is present
	if h.APIKey == "" {
		return h.mockCheck(ctx, domain)
	}

	// Hostinger API calls (if key was provided)
	return h.mockCheck(ctx, domain)
}

func (h *HostingerProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if h.APIKey == "" {
		return h.mockPrice(ctx, domain)
	}

	return h.mockPrice(ctx, domain)
}

func (h *HostingerProvider) mockCheck(ctx context.Context, domain string) (*DomainResult, error) {
	hf := fnv.New32a()
	hf.Write([]byte(domain))
	hash := hf.Sum32()

	r := rand.New(rand.NewSource(int64(hash) + 234))
	latency := time.Duration(r.Intn(40)+20) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	available := (hash % 10) < 6
	parts := strings.Split(domain, ".")
	if len(parts) > 0 && len(parts[0]) <= 5 {
		available = (hash % 10) < 2
	}

	return &DomainResult{
		Domain:    domain,
		Available: available,
	}, nil
}

func (h *HostingerProvider) mockPrice(ctx context.Context, domain string) (*PriceResult, error) {
	parts := strings.Split(domain, ".")
	tld := "com"
	if len(parts) >= 2 {
		tld = parts[len(parts)-1]
	}

	hf := fnv.New32a()
	hf.Write([]byte(domain))
	hash := hf.Sum32()

	r := rand.New(rand.NewSource(int64(hash) + 567))
	latency := time.Duration(r.Intn(20)+10) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	var price float64
	// Hostinger is known to be slightly cheaper than GoDaddy
	switch strings.ToLower(tld) {
	case "com":
		price = 749.00
	case "in":
		price = 399.00
	case "net":
		price = 899.00
	case "org":
		price = 849.00
	case "ai":
		price = 4999.00
	case "io":
		price = 3199.00
	default:
		price = 999.00
	}

	variation := float64(int(hash%150) - 40)
	price += variation

	if (hash % 10) == 0 {
		price += float64(r.Intn(9000) + 4000)
	}

	return &PriceResult{
		Price:    price,
		Currency: "INR",
		Platform: "Hostinger",
	}, nil
}
