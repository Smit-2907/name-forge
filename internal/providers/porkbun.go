package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type PorkbunProvider struct {
	APIKey    string
	SecretKey string
	Client    *http.Client
}

func NewPorkbunProvider(apiKey, secretKey string) *PorkbunProvider {
	return &PorkbunProvider{
		APIKey:    apiKey,
		SecretKey: secretKey,
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

type porkbunReq struct {
	APIKey    string `json:"apikey"`
	SecretKey string `json:"secretkey"`
}

type porkbunCheckResp struct {
	Status    string `json:"status"`    // "SUCCESS"
	Available string `json:"available"` // "yes" or "no"
	Message   string `json:"message"`
}

type porkbunPricingResp struct {
	Status  string                           `json:"status"`
	Pricing map[string]porkbunTLDPricingItem `json:"pricing"`
}

type porkbunTLDPricingItem struct {
	Registration string `json:"registration"`
}

func (p *PorkbunProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	if p.APIKey == "" || p.SecretKey == "" {
		return KeylessCheckAvailability(ctx, p.Client, domain)
	}

	targetURL := fmt.Sprintf("https://porkbun.com/api/json/v3/domain/check/%s", url.PathEscape(domain))
	reqData := porkbunReq{
		APIKey:    p.APIKey,
		SecretKey: p.SecretKey,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("porkbun check returned status: %d", resp.StatusCode)
	}

	var checkResult porkbunCheckResp
	if err := json.NewDecoder(resp.Body).Decode(&checkResult); err != nil {
		return nil, err
	}

	if strings.ToLower(checkResult.Status) != "success" {
		return nil, fmt.Errorf("porkbun check failed: %s", checkResult.Message)
	}

	return &DomainResult{
		Domain:    domain,
		Available: strings.ToLower(checkResult.Available) == "yes",
	}, nil
}

func (p *PorkbunProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if p.APIKey == "" || p.SecretKey == "" {
		return p.fallbackPrice(ctx, domain)
	}

	// For pricing, Porkbun typically returns a list of TLD prices.
	// We extract the TLD from the domain and lookup the cost.
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return &PriceResult{Price: 12.00, Currency: "USD", Platform: "Porkbun"}, nil
	}
	tld := parts[len(parts)-1]

	pricingURL := "https://porkbun.com/api/json/v3/domain/pricing"
	reqData := porkbunReq{
		APIKey:    p.APIKey,
		SecretKey: p.SecretKey,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", pricingURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("porkbun pricing returned status: %d", resp.StatusCode)
	}

	var pricingResult porkbunPricingResp
	if err := json.NewDecoder(resp.Body).Decode(&pricingResult); err != nil {
		return nil, err
	}

	if strings.ToLower(pricingResult.Status) != "success" {
		return nil, fmt.Errorf("porkbun pricing lookup failed")
	}

	item, exists := pricingResult.Pricing[tld]
	if !exists {
		// Fallback pricing if TLD is missing from response list
		var plans []PricePlan
		plans = append(plans, PricePlan{Name: "Porkbun (1-Yr Domain Only)", Price: 15.00, Currency: "USD"})
		plans = append(plans, PricePlan{Name: "Porkbun (2-Yr Term Avg)", Price: 15.00 * 1.1, Currency: "USD"})
		plans = append(plans, PricePlan{Name: "Porkbun (Domain + Hosting)", Price: 15.00 + 1.50, Currency: "USD"})
		return &PriceResult{Price: 15.00, Currency: "USD", Platform: "Porkbun", Plans: plans}, nil
	}

	var price float64
	_, err = fmt.Sscanf(item.Registration, "%f", &price)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to parse Porkbun price for TLD %s: %s", tld, item.Registration)
		price = 15.00
	}

	var plans []PricePlan
	plans = append(plans, PricePlan{
		Name:     "Porkbun (1-Yr Domain Only)",
		Price:    price,
		Currency: "USD",
	})
	plans = append(plans, PricePlan{
		Name:     "Porkbun (2-Yr Term Avg)",
		Price:    price * 1.1,
		Currency: "USD",
	})
	plans = append(plans, PricePlan{
		Name:     "Porkbun (Domain + Hosting)",
		Price:    price + 1.50,
		Currency: "USD",
	})

	return &PriceResult{
		Price:    price,
		Currency: "USD",
		Platform: "Porkbun",
		Plans:    plans,
	}, nil
}

func (p *PorkbunProvider) fallbackPrice(ctx context.Context, domain string) (*PriceResult, error) {
	parts := strings.Split(domain, ".")
	tld := "com"
	if len(parts) >= 2 {
		tld = parts[len(parts)-1]
	}

	var price float64
	switch strings.ToLower(tld) {
	case "com":
		price = 10.37
	case "net":
		price = 12.50
	case "org":
		price = 12.50
	case "ai":
		price = 58.90
	case "io":
		price = 34.85
	case "in":
		price = 7.50
	default:
		price = 12.00
	}

	var plans []PricePlan
	plans = append(plans, PricePlan{
		Name:     "Porkbun (1-Yr Domain Only)",
		Price:    price,
		Currency: "USD",
	})
	plans = append(plans, PricePlan{
		Name:     "Porkbun (2-Yr Term Avg)",
		Price:    price * 1.1,
		Currency: "USD",
	})
	plans = append(plans, PricePlan{
		Name:     "Porkbun (Domain + Hosting)",
		Price:    price + 1.50,
		Currency: "USD",
	})

	return &PriceResult{
		Price:    price,
		Currency: "USD",
		Platform: "Porkbun",
		Plans:    plans,
	}, nil
}
