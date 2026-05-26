package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
		Client:    &http.Client{Timeout: 5 * time.Second},
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
		return nil, fmt.Errorf("porkbun provider credentials missing")
	}

	url := fmt.Sprintf("https://porkbun.com/api/json/v3/domain/check/%s", domain)
	reqData := porkbunReq{
		APIKey:    p.APIKey,
		SecretKey: p.SecretKey,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
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
		return nil, fmt.Errorf("porkbun provider credentials missing")
	}

	// For pricing, Porkbun typically returns a list of TLD prices.
	// We extract the TLD from the domain and lookup the cost.
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return &PriceResult{Price: 12.00, Currency: "USD", Platform: "Porkbun"}, nil
	}
	tld := parts[len(parts)-1]

	url := "https://porkbun.com/api/json/v3/domain/pricing"
	reqData := porkbunReq{
		APIKey:    p.APIKey,
		SecretKey: p.SecretKey,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
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
		return &PriceResult{Price: 15.00, Currency: "USD", Platform: "Porkbun"}, nil
	}

	var price float64
	_, err = fmt.Sscanf(item.Registration, "%f", &price)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to parse Porkbun price for TLD %s: %s", tld, item.Registration)
		price = 15.00
	}

	return &PriceResult{
		Price:    price,
		Currency: "USD", Platform: "Porkbun",
	}, nil
}
