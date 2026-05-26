package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

type GoDaddyProvider struct {
	APIKey    string
	APISecret string
	Client    *http.Client
}

func NewGoDaddyProvider(apiKey, apiSecret string) *GoDaddyProvider {
	return &GoDaddyProvider{
		APIKey:    apiKey,
		APISecret: apiSecret,
		Client:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (g *GoDaddyProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	if g.APIKey == "" || g.APISecret == "" {
		// Mock Mode
		return g.mockCheck(ctx, domain)
	}

	url := fmt.Sprintf("https://api.godaddy.com/v1/domains/available?domain=%s", domain)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", g.APIKey, g.APISecret))
	req.Header.Set("Accept", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("godaddy check returned status: %d", resp.StatusCode)
	}

	var godaddyResp struct {
		Available bool   `json:"available"`
		Domain    string `json:"domain"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&godaddyResp); err != nil {
		return nil, err
	}

	return &DomainResult{
		Domain:    domain,
		Available: godaddyResp.Available,
	}, nil
}

func (g *GoDaddyProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if g.APIKey == "" || g.APISecret == "" {
		// Mock Mode
		return g.mockPrice(ctx, domain)
	}

	// GoDaddy requires query parameters or schema schema
	url := fmt.Sprintf("https://api.godaddy.com/v1/domains/available?domain=%s", domain)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", g.APIKey, g.APISecret))
	req.Header.Set("Accept", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("godaddy pricing returned status: %d", resp.StatusCode)
	}

	var godaddyResp struct {
		Price    int    `json:"price"` // Micro-units (e.g. 1000000 = 1.00)
		Currency string `json:"currency"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&godaddyResp); err != nil {
		return nil, err
	}

	// Convert GoDaddy's micro-units price
	priceVal := float64(godaddyResp.Price) / 1000000.0
	if priceVal == 0 {
		priceVal = 12.99
		godaddyResp.Currency = "USD"
	}

	return &PriceResult{
		Price:    priceVal,
		Currency: godaddyResp.Currency,
		Platform: "GoDaddy",
	}, nil
}

func (g *GoDaddyProvider) mockCheck(ctx context.Context, domain string) (*DomainResult, error) {
	h := fnv.New32a()
	h.Write([]byte(domain))
	hash := h.Sum32()

	// Add slight simulated latency
	r := rand.New(rand.NewSource(int64(hash) + 123))
	latency := time.Duration(r.Intn(40)+20) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	// Deterministic availability matching the mock provider
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

func (g *GoDaddyProvider) mockPrice(ctx context.Context, domain string) (*PriceResult, error) {
	parts := strings.Split(domain, ".")
	tld := "com"
	if len(parts) >= 2 {
		tld = parts[len(parts)-1]
	}

	h := fnv.New32a()
	h.Write([]byte(domain))
	hash := h.Sum32()

	r := rand.New(rand.NewSource(int64(hash) + 456))
	latency := time.Duration(r.Intn(20)+10) * time.Millisecond

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(latency):
	}

	var price float64
	// GoDaddy pricing in INR
	switch strings.ToLower(tld) {
	case "com":
		price = 899.00
	case "in":
		price = 449.00
	case "net":
		price = 999.00
	case "org":
		price = 949.00
	case "ai":
		price = 5299.00
	case "io":
		price = 3299.00
	default:
		price = 1099.00
	}

	// Add deterministic minor variation: e.g. -₹50 to +₹150
	variation := float64(int(hash%200) - 50)
	price += variation

	// Premium domain simulation
	if (hash % 10) == 0 {
		price += float64(r.Intn(10000) + 5000)
	}

	return &PriceResult{
		Price:    price,
		Currency: "INR",
		Platform: "GoDaddy",
	}, nil
}
