package providers

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type NamecheapProvider struct {
	ApiUser  string
	ApiKey   string
	ClientIP string
	Client   *http.Client
}

func NewNamecheapProvider(apiUser, apiKey, clientIP string) *NamecheapProvider {
	return &NamecheapProvider{
		ApiUser:  apiUser,
		ApiKey:   apiKey,
		ClientIP: clientIP,
		Client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// XML structures for parsing Namecheap response

type NamecheapApiResponse struct {
	XMLName         xml.Name `xml:"ApiResponse"`
	Status          string   `xml:"Status,attr"`
	CommandResponse struct {
		DomainCheckResult struct {
			Domain       string  `xml:"Domain,attr"`
			Available    string  `xml:"Available,attr"`
			IsPremium    string  `xml:"IsPremiumName,attr"`
			PremiumPrice float64 `xml:"PremiumRegistrationPrice,attr"`
		} `xml:"DomainCheckResult"`
		GetPricingResult struct {
			ProductType struct {
				ProductCategory []struct {
					Name    string `xml:"Name,attr"` // "register"
					Product []struct {
						Name  string `xml:"Name,attr"` // e.g. "com", "net"
						Price []struct {
							Price float64 `xml:"Price,attr"`
						} `xml:"Price"`
					} `xml:"Product"`
				} `xml:"ProductCategory"`
			} `xml:"ProductType"`
		} `xml:"GetPricingResult"`
	} `xml:"CommandResponse"`
	Errors struct {
		Error []struct {
			Number      int    `xml:"Number,attr"`
			Description string `xml:",chardata"`
		} `xml:"Error"`
	} `xml:"Errors"`
}

func (n *NamecheapProvider) CheckAvailability(ctx context.Context, domain string) (*DomainResult, error) {
	if n.ApiUser == "" || n.ApiKey == "" {
		return nil, fmt.Errorf("namecheap provider credentials missing")
	}

	endpoint := "https://api.namecheap.com/xml.response"
	params := url.Values{}
	params.Set("ApiUser", n.ApiUser)
	params.Set("ApiKey", n.ApiKey)
	params.Set("UserName", n.ApiUser)
	params.Set("Command", "namecheap.domains.check")
	params.Set("ClientIp", n.ClientIP)
	params.Set("DomainList", domain)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := n.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp NamecheapApiResponse
	if err := xml.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if strings.ToLower(apiResp.Status) != "ok" && len(apiResp.Errors.Error) > 0 {
		return nil, fmt.Errorf("namecheap check error %d: %s",
			apiResp.Errors.Error[0].Number, apiResp.Errors.Error[0].Description)
	}

	available, _ := strconv.ParseBool(apiResp.CommandResponse.DomainCheckResult.Available)

	return &DomainResult{
		Domain:    domain,
		Available: available,
	}, nil
}

func (n *NamecheapProvider) GetPrice(ctx context.Context, domain string) (*PriceResult, error) {
	if n.ApiUser == "" || n.ApiKey == "" {
		return nil, fmt.Errorf("namecheap provider credentials missing")
	}

	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return &PriceResult{Price: 12.00, Currency: "USD", Platform: "Namecheap"}, nil
	}
	tld := parts[len(parts)-1]

	endpoint := "https://api.namecheap.com/xml.response"
	params := url.Values{}
	params.Set("ApiUser", n.ApiUser)
	params.Set("ApiKey", n.ApiKey)
	params.Set("UserName", n.ApiUser)
	params.Set("Command", "namecheap.domains.getPricing")
	params.Set("ClientIp", n.ClientIP)
	params.Set("ProductType", "DOMAIN")
	params.Set("ProductCategory", "REGISTER")
	params.Set("ActionName", "REGISTER")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := n.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp NamecheapApiResponse
	if err := xml.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if strings.ToLower(apiResp.Status) != "ok" && len(apiResp.Errors.Error) > 0 {
		return nil, fmt.Errorf("namecheap pricing error %d: %s",
			apiResp.Errors.Error[0].Number, apiResp.Errors.Error[0].Description)
	}

	// Drill down the XML to find registration price for the TLD
	for _, cat := range apiResp.CommandResponse.GetPricingResult.ProductType.ProductCategory {
		if strings.ToLower(cat.Name) == "register" {
			for _, prod := range cat.Product {
				if strings.ToLower(prod.Name) == strings.ToLower(tld) {
					if len(prod.Price) > 0 {
						return &PriceResult{
							Price:    prod.Price[0].Price,
							Currency: "USD", Platform: "Namecheap",
						}, nil
					}
				}
			}
		}
	}

	// Fallback price if XML lookup fails
	return &PriceResult{Price: 14.99, Currency: "USD", Platform: "Namecheap"}, nil
}
