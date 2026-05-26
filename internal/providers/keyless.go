package providers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var defaultHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

// Static cache of common TLD WHOIS servers
var (
	tldWhoisMap = map[string]string{
		"com": "whois.verisign-grs.com",
		"net": "whois.verisign-grs.com",
		"org": "whois.pir.org",
		"co":  "whois.nic.co",
		"io":  "whois.nic.io",
		"ai":  "whois.nic.ai",
		// .in WHOIS servers (whois.registry.in / whois.inregistry.net) both resolve
		// to Cloudflare IPs that block port 43. Omitting .in from this map causes
		// the code to skip WHOIS for .in and use the DNS-trusting final fallback.
		"us": "whois.nic.us",
		"uk": "whois.nic.uk",
		"ca": "whois.cira.ca",
		"de": "whois.denic.de",
		"au": "whois.auda.org.au",
		"jp": "whois.jprs.jp",
	}
	tldWhoisMu sync.RWMutex
)

// QueryWhois queries a WHOIS server over TCP port 43.
func QueryWhois(ctx context.Context, query string, server string) (string, error) {
	// Set a strict dial context limit of 1200ms to avoid blocking on firewall limits
	dialCtx, dialCancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer dialCancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", net.JoinHostPort(server, "43"))
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// Set a short deadline for writing/reading
	conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))

	_, err = conn.Write([]byte(query + "\r\n"))
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			break
		}
	}

	return buf.String(), nil
}

// GetWhoisServer resolves the WHOIS server for a given domain/TLD.
func GetWhoisServer(ctx context.Context, domain string) (string, error) {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid domain for TLD parsing: %s", domain)
	}
	tld := strings.ToLower(parts[len(parts)-1])

	tldWhoisMu.RLock()
	server, exists := tldWhoisMap[tld]
	tldWhoisMu.RUnlock()
	if exists {
		return server, nil
	}

	// Dynamic lookup via IANA WHOIS server
	log.Debug().Msgf("TLD %s not in static WHOIS map, querying whois.iana.org...", tld)
	ianaResp, err := QueryWhois(ctx, tld, "whois.iana.org")
	if err != nil {
		return "", err
	}

	lines := strings.Split(ianaResp, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "whois:") {
			colParts := strings.SplitN(trimmed, ":", 2)
			if len(colParts) == 2 {
				server = strings.TrimSpace(colParts[1])
				if server != "" {
					tldWhoisMu.Lock()
					tldWhoisMap[tld] = server
					tldWhoisMu.Unlock()
					log.Info().Msgf("Resolved and cached WHOIS server for TLD %s: %s", tld, server)
					return server, nil
				}
			}
		}
	}

	return "", fmt.Errorf("whois server not found in IANA response for TLD %s", tld)
}

// KeylessCheckAvailability checks domain availability without any API keys.
// It uses a highly efficient, multi-phase check:
// 1. Fast DNS pre-check (Resolves IPs, NS, MX records in parallel with 1s timeout).
// 2. Direct RDAP query via authoritative registry endpoints (avoiding port 43 timeouts).
// 3. Fallback to WHOIS TCP query on port 43 only if RDAP fails.
func KeylessCheckAvailability(ctx context.Context, client *http.Client, domain string) (*DomainResult, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	domain = strings.ToLower(strings.TrimSpace(domain))

	// Phase 1: Concurrently check DNS records with a 1-second timeout
	dnsCtx, dnsCancel := context.WithTimeout(ctx, 1000*time.Millisecond)
	defer dnsCancel()

	var isRegistered bool
	var dnsWg sync.WaitGroup
	var dnsMu sync.Mutex

	dnsWg.Add(3)

	go func() {
		defer dnsWg.Done()
		ips, err := net.DefaultResolver.LookupHost(dnsCtx, domain)
		if err == nil && len(ips) > 0 {
			dnsMu.Lock()
			isRegistered = true
			dnsMu.Unlock()
		}
	}()

	go func() {
		defer dnsWg.Done()
		ns, err := net.DefaultResolver.LookupNS(dnsCtx, domain)
		if err == nil && len(ns) > 0 {
			dnsMu.Lock()
			isRegistered = true
			dnsMu.Unlock()
		}
	}()

	go func() {
		defer dnsWg.Done()
		mx, err := net.DefaultResolver.LookupMX(dnsCtx, domain)
		if err == nil && len(mx) > 0 {
			dnsMu.Lock()
			isRegistered = true
			dnsMu.Unlock()
		}
	}()

	dnsWg.Wait()

	if isRegistered {
		log.Debug().Msgf("DNS pre-check: domain %s is registered", domain)
		return &DomainResult{Domain: domain, Available: false}, nil
	}

	// Phase 2: Direct RDAP Check (Fast and reliable over HTTPS port 443)
	log.Debug().Msgf("Querying RDAP for domain %s...", domain)
	rdapCtx, rdapCancel := context.WithTimeout(ctx, 2*time.Second)
	defer rdapCancel()

	targetURL := GetAuthoritativeRdapURL(domain)
	req, err := http.NewRequestWithContext(rdapCtx, "GET", targetURL, nil)
	var rdapErr error
	var rdapAvailable bool
	var rdapChecked bool

	if err == nil {
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				rdapAvailable = true
				rdapChecked = true
				log.Debug().Msgf("RDAP check: domain %s is available (404)", domain)
			} else if resp.StatusCode == http.StatusOK {
				rdapAvailable = false
				rdapChecked = true
				log.Debug().Msgf("RDAP check: domain %s is taken (200)", domain)
			} else if resp.StatusCode == http.StatusTooManyRequests {
				// Rate-limited by the RDAP proxy — fall through to WHOIS
				rdapErr = fmt.Errorf("RDAP rate-limited (429) for %s", domain)
				log.Debug().Msgf("RDAP rate-limited for %s, will try WHOIS", domain)
			} else {
				rdapErr = fmt.Errorf("RDAP returned status code %d", resp.StatusCode)
			}
		} else {
			rdapErr = err
		}
	} else {
		rdapErr = err
	}

	if rdapChecked {
		return &DomainResult{Domain: domain, Available: rdapAvailable}, nil
	}

	log.Warn().Err(rdapErr).Msgf("RDAP query failed for %s, falling back to WHOIS port 43", domain)

	// Phase 3: Fallback to WHOIS TCP port 43 check (with fast failover)
	whoisServer, err := GetWhoisServer(ctx, domain)
	if err == nil && whoisServer != "" {
		log.Debug().Msgf("Querying WHOIS server %s for domain %s...", whoisServer, domain)
		whoisOut, err := QueryWhois(ctx, domain, whoisServer)
		if err == nil {
			lowerOut := strings.ToLower(whoisOut)
			availablePatterns := []string{
				"no match for",
				"not found",
				"no object found",
				"not registered",
				"no entries found",
				"is free",
				"available",
				"status: free",
				"no data found",
				"no match",
				"incorrect domain name",
				"query returned 0 objects",
				"no registered database record",
				"domain not found",
				"no entries",
				"no such domain",
				"el dominio no existe",
				"domain registration free",
				"nothing found",
			}
			available := false
			for _, pat := range availablePatterns {
				if strings.Contains(lowerOut, pat) {
					available = true
					break
				}
			}
			log.Debug().Msgf("WHOIS check fallback for %s returned available: %v", domain, available)
			return &DomainResult{Domain: domain, Available: available}, nil
		}
		log.Warn().Err(err).Msgf("WHOIS fallback failed for %s", domain)
	}

	// Final fallback: DNS pre-check already confirmed no A/NS/MX records exist.
	// If RDAP and WHOIS both timed out (common in containerised environments where
	// port 43 and some RDAP proxies are blocked), we trust the DNS result.
	// A domain with no DNS records is very likely available — a taken domain
	// almost always has at least nameserver (NS) records.
	log.Debug().Msgf("All registry checks timed out for %s; DNS showed no records — assuming available", domain)
	return &DomainResult{Domain: domain, Available: true}, nil
}

// GetAuthoritativeRdapURL resolves the direct registry RDAP service URL to avoid central portal rate limits
func GetAuthoritativeRdapURL(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return "https://rdap.org/domain/" + domain
	}
	tld := strings.ToLower(parts[len(parts)-1])

	// Use rdap.org as a universal proxy — it bootstraps from IANA and is reliably
	// reachable from containerised environments where direct registry RDAP endpoints
	// (e.g. rdap.nixiregistry.in, rdap.verisign.com) may time out due to DNS or
	// firewall restrictions.
	//
	// Specific overrides are only kept where the registry endpoint is known to be
	// stable and publicly routable from server environments.
	switch tld {
	case "co":
		// nic.co RDAP is reliable and faster than the rdap.org proxy
		return "https://rdap.nic.co/domain/" + domain
	default:
		// rdap.org proxies to the authoritative registry using IANA bootstrap data
		return "https://rdap.org/domain/" + domain
	}
}
