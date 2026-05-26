package providers

import (
	"context"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type BigRockProvider struct {
	ResellerID string
	APIKey     string
	Client     *http.Client
}

func NewBigRockProvider(resellerID, apiKey string) *BigRockProvider {
	return &BigRockProvider{
		ResellerID: resellerID,
		APIKey:     apiKey,
		Client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (b *BigRockProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	if b.APIKey == "" {
		return KeylessCheckAvailability(ctx, b.Client, domain)
	}
	return b.mockCheck(ctx, domain)
}

func (b *BigRockProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if b.APIKey == "" {
		return b.mockPrice(ctx, domain)
	}

	return b.mockPrice(ctx, domain)
}

func (b *BigRockProvider) mockCheck(ctx context.Context, domain string) (*DomainResult, error) {
	hf := fnv.New32a()
	hf.Write([]byte(domain))
	hash := hf.Sum32()

	r := rand.New(rand.NewSource(int64(hash) + 345))
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

func (b *BigRockProvider) mockPrice(ctx context.Context, domain string) (*PriceResult, error) {
	parts := strings.Split(domain, ".")
	tld := "com"
	if len(parts) >= 2 {
		tld = parts[len(parts)-1]
	}

	hf := fnv.New32a()
	hf.Write([]byte(domain))
	hash := hf.Sum32()

	r := rand.New(rand.NewSource(int64(hash) + 678))
	latency := time.Duration(r.Intn(20)+10) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	var price float64
	// BigRock standard Indian registration pricing
	switch strings.ToLower(tld) {
	case "com":
		price = 929.00
	case "in":
		price = 459.00
	case "net":
		price = 1029.00
	case "org":
		price = 959.00
	case "ai":
		price = 5499.00
	case "io":
		price = 3399.00
	default:
		price = 1129.00
	}

	variation := float64(int(hash%220) - 60)
	price += variation

	if (hash % 10) == 0 {
		price += float64(r.Intn(11000) + 6000)
	}

	var plans []PricePlan
	plans = append(plans, PricePlan{
		Name:     "BigRock (1-Yr Domain Only)",
		Price:    price,
		Currency: "INR",
	})
	plans = append(plans, PricePlan{
		Name:     "BigRock (2-Yr Term Avg)",
		Price:    price * 1.2,
		Currency: "INR",
	})
	plans = append(plans, PricePlan{
		Name:     "BigRock (Domain + Email Plan)",
		Price:    price + 348.00,
		Currency: "INR",
	})

	return &PriceResult{
		Price:    price,
		Currency: "INR",
		Platform: "BigRock",
		Plans:    plans,
	}, nil
}
