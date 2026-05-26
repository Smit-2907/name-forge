package providers

import (
	"context"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"
)

type MockProvider struct {
	platform string
	currency string
	baseCOM  float64
	baseIN   float64
	baseAI   float64
	baseIO   float64
}

func NewMockProvider(platform, currency string, baseCOM, baseIN, baseAI, baseIO float64) *MockProvider {
	return &MockProvider{
		platform: platform,
		currency: currency,
		baseCOM:  baseCOM,
		baseIN:   baseIN,
		baseAI:   baseAI,
		baseIO:   baseIO,
	}
}

func getDeterministicHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func (m *MockProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	// Simulate networking latency: between 60ms and 180ms
	hash := getDeterministicHash(domain)
	r := rand.New(rand.NewSource(int64(hash)))
	latency := time.Duration(r.Intn(120)+60) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	// 60% of domains are available deterministically
	available := (hash % 10) < 6

	// Exception: extremely short domains (<6 chars) are taken
	parts := strings.Split(domain, ".")
	if len(parts) > 0 && len(parts[0]) <= 5 {
		available = (hash % 10) < 2 // Only 20% available for short domains
	}

	return &DomainResult{
		Domain:    domain,
		Available: available,
	}, nil
}

func (m *MockProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	parts := strings.Split(domain, ".")
	tld := "com"
	if len(parts) >= 2 {
		tld = parts[len(parts)-1]
	}

	hash := getDeterministicHash(domain)
	r := rand.New(rand.NewSource(int64(hash) + 1))
	latency := time.Duration(r.Intn(40)+20) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	// Standard pricing structures based on TLD and custom provider configuration
	var price float64
	switch strings.ToLower(tld) {
	case "com":
		price = m.baseCOM
	case "in":
		price = m.baseIN
	case "ai":
		price = m.baseAI
	case "io":
		price = m.baseIO
	case "net":
		if m.currency == "INR" {
			price = m.baseCOM + 100.0
		} else {
			price = m.baseCOM + 2.0
		}
	case "org":
		if m.currency == "INR" {
			price = m.baseCOM + 50.0
		} else {
			price = m.baseCOM + 1.0
		}
	default:
		if m.currency == "INR" {
			price = m.baseCOM + 200.0
		} else {
			price = m.baseCOM + 3.0
		}
	}

	// Add deterministic variation based on hash
	var variation float64
	if m.currency == "INR" {
		variation = float64(int(hash%150) - 50) // -50 to +100 INR
	} else {
		variation = float64(int(hash%200)-100) / 100.0 // -1.00 to +1.00 USD
	}
	price += variation

	// 10% chance it is a "premium domain" with boosted pricing
	if (hash % 10) == 0 {
		if m.currency == "INR" {
			price += float64(r.Intn(15000) + 5000)
		} else {
			price += float64(r.Intn(200) + 50)
		}
	}

	var plans []PricePlan
	plans = append(plans, PricePlan{
		Name:     m.platform + " (1-Yr Domain Only)",
		Price:    price,
		Currency: m.currency,
	})
	plans = append(plans, PricePlan{
		Name:     m.platform + " (2-Yr Term Avg)",
		Price:    price * 1.25,
		Currency: m.currency,
	})
	plans = append(plans, PricePlan{
		Name:     m.platform + " (Premium Bundle)",
		Price:    price * 1.5,
		Currency: m.currency,
	})

	return &PriceResult{
		Price:    price,
		Currency: m.currency,
		Platform: m.platform,
		Plans:    plans,
	}, nil
}
