package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"net/url"
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
		Client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (g *GoDaddyProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	if g.APIKey == "" || g.APISecret == "" {
		return KeylessCheckAvailability(ctx, g.Client, domain)
	}

	targetURL := fmt.Sprintf("https://api.godaddy.com/v1/domains/available?domain=%s", url.QueryEscape(domain))
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
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

func getGoDaddyPricingContext(tld string, hpPrice float64) (promoPrice, renewalPrice, standalone1Yr float64) {
	switch strings.ToLower(tld) {
	case "com":
		// GoDaddy .com promo: ₹499 (requires 2-yr registration), renewal: ₹1399, standalone 1-yr: ₹1199
		return 499.00, 1399.00, 1199.00
	case "in":
		// GoDaddy .in promo: ₹399 (requires 2-yr registration), renewal: ₹699, standalone 1-yr: ₹699
		return 399.00, 699.00, 699.00
	case "net":
		// GoDaddy .net promo: ₹799, renewal: ₹1599, standalone 1-yr: ₹1399
		return 799.00, 1599.00, 1399.00
	case "org":
		// GoDaddy .org promo: ₹799, renewal: ₹1399, standalone 1-yr: ₹1299
		return 799.00, 1399.00, 1299.00
	case "ai":
		// GoDaddy .ai is expensive and rarely has promos, standard price is around ₹5999
		return 5999.00, 5999.00, 5999.00
	case "io":
		// GoDaddy .io standard price is around ₹3799
		return 3799.00, 3799.00, 3799.00
	default:
		// Default fallback based on Hostinger price or base rate
		base := hpPrice
		if base <= 0 {
			base = 999.00
		}
		return base, base * 1.5, base * 1.3
	}
}

func (g *GoDaddyProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if g.APIKey == "" || g.APISecret == "" {
		// Keyless flow: Query Hostinger live prices (if not circuit-broken) and scale them
		var hpVal float64 = 0
		parts := strings.Split(domain, ".")
		tld := "com"
		if len(parts) >= 2 {
			tld = strings.ToLower(parts[len(parts)-1])
		}

		if !IsScraperTripped() {
			hp := NewHostingerProvider("")
			hpPrice, err := hp.getLivePrice(ctx, domain)
			if err == nil && hpPrice != nil {
				hpVal = hpPrice.Price
				RecordScraperSuccess()
			} else {
				RecordScraperFailure()
			}
		}

		promo, renewal, standalone := getGoDaddyPricingContext(tld, hpVal)

		var plans []PricePlan
		plans = append(plans, PricePlan{
			Name:     "GoDaddy (1-Yr Domain Only)",
			Price:    standalone,
			Currency: "INR",
		})
		plans = append(plans, PricePlan{
			Name:     "GoDaddy (2-Yr Term Avg)",
			Price:    (promo + renewal) / 2,
			Currency: "INR",
		})
		plans = append(plans, PricePlan{
			Name:     "GoDaddy (Domain + Email Plan)",
			Price:    promo + 348.00,
			Currency: "INR",
		})

		return &PriceResult{
			Price:    standalone,
			Currency: "INR",
			Platform: "GoDaddy",
			Plans:    plans,
		}, nil
	}

	// GoDaddy official API flow
	targetURL := fmt.Sprintf("https://api.godaddy.com/v1/domains/available?domain=%s", url.QueryEscape(domain))
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
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

	// Build plans for official API
	var plans []PricePlan
	emailPrice := 4.00
	if godaddyResp.Currency == "INR" {
		emailPrice = 348.00
	}
	
	// Estimate standard, renewal and promo based on the retrieved priceVal
	var renewalVal float64
	var promoVal float64
	var standaloneVal float64
	if godaddyResp.Currency == "INR" {
		parts := strings.Split(domain, ".")
		tld := "com"
		if len(parts) >= 2 {
			tld = strings.ToLower(parts[len(parts)-1])
		}
		promoVal, renewalVal, standaloneVal = getGoDaddyPricingContext(tld, priceVal)
	} else {
		// Fallback for USD/other currencies
		promoVal = priceVal * 0.7 // approx promo discount
		renewalVal = priceVal * 1.5
		standaloneVal = priceVal
	}

	plans = append(plans, PricePlan{
		Name:     "GoDaddy (1-Yr Domain Only)",
		Price:    standaloneVal,
		Currency: godaddyResp.Currency,
	})
	plans = append(plans, PricePlan{
		Name:     "GoDaddy (2-Yr Term Avg)",
		Price:    (promoVal + renewalVal) / 2,
		Currency: godaddyResp.Currency,
	})
	plans = append(plans, PricePlan{
		Name:     "GoDaddy (Domain + Email Plan)",
		Price:    promoVal + emailPrice,
		Currency: godaddyResp.Currency,
	})

	return &PriceResult{
		Price:    standaloneVal,
		Currency: godaddyResp.Currency,
		Platform: "GoDaddy",
		Plans:    plans,
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

	// Retrieve TLD specific plans
	promo, renewal, standalone := getGoDaddyPricingContext(tld, 0)

	// Add deterministic minor variation: e.g. -₹50 to +₹150
	variation := float64(int(hash%200) - 50)
	promo += variation
	standalone += variation
	renewal += variation

	// Premium domain simulation
	if (hash % 10) == 0 {
		premiumAdd := float64(r.Intn(10000) + 5000)
		promo += premiumAdd
		standalone += premiumAdd
		renewal += premiumAdd
	}

	var plans []PricePlan
	plans = append(plans, PricePlan{
		Name:     "GoDaddy (1-Yr Domain Only)",
		Price:    standalone,
		Currency: "INR",
	})
	plans = append(plans, PricePlan{
		Name:     "GoDaddy (2-Yr Term Avg)",
		Price:    (promo + renewal) / 2,
		Currency: "INR",
	})
	plans = append(plans, PricePlan{
		Name:     "GoDaddy (Domain + Email Plan)",
		Price:    promo + 348.00,
		Currency: "INR",
	})

	return &PriceResult{
		Price:    standalone,
		Currency: "INR",
		Platform: "GoDaddy",
		Plans:    plans,
	}, nil
}
