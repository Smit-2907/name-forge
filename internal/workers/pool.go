package workers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"nameforge/internal/cache"
	"nameforge/internal/models"
	"nameforge/internal/providers"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

type CheckJob struct {
	NameID int64  // Linked name ID (if stored in DB)
	Name   string // Base name (e.g. "Veltrix")
	Domain string // Full domain (e.g. "veltrix.ai")
	TLD    string // TLD (e.g. ".ai")
}

type CheckResult struct {
	NameID    int64
	Name      string
	Domain    string
	TLD       string
	Available bool
	Price     float64
	Currency  string
	Platform  string
	Offers    []models.ProviderOffer
	Cached    bool
	Error     error
}

// providerHealth tracks the failure rate of a provider for circuit-breaking
type providerHealth struct {
	failures     int
	lastFailure  time.Time
	tripped      bool
	trippedUntil time.Time
}

// tokenBucket restricts the request frequency to a provider
type tokenBucket struct {
	mu           sync.Mutex
	tokens       float64
	capacity     float64
	fillRate     float64 // tokens per second
	lastRefilled time.Time
}

func newTokenBucket(capacity, fillRate float64) *tokenBucket {
	return &tokenBucket{
		tokens:       capacity,
		capacity:     capacity,
		fillRate:     fillRate,
		lastRefilled: time.Now(),
	}
}

func (tb *tokenBucket) Take() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefilled).Seconds()
	tb.lastRefilled = now

	tb.tokens += elapsed * tb.fillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// WorkerPool handles concurrent execution of domain check jobs.
type WorkerPool struct {
	providers  []providers.DomainProvider
	cacheSvc   *cache.CacheService
	cacheTTL   time.Duration
	sfGroup    singleflight.Group
	priceSfGrp singleflight.Group
	healthMu   sync.Mutex
	health     map[string]*providerHealth
	throttleMu sync.Mutex
	throttlers map[string]*tokenBucket
}

func NewWorkerPool(providers []providers.DomainProvider, cacheSvc *cache.CacheService, cacheTTL time.Duration) *WorkerPool {
	return &WorkerPool{
		providers:  providers,
		cacheSvc:   cacheSvc,
		cacheTTL:   cacheTTL,
		health:     make(map[string]*providerHealth),
		throttlers: make(map[string]*tokenBucket),
	}
}

// RunChecks runs a batch of domain checks concurrently using a pool of N goroutines.
func (wp *WorkerPool) RunChecks(ctx context.Context, jobs []CheckJob, concurrency int) []CheckResult {
	if len(jobs) == 0 {
		return nil
	}

	jobChan := make(chan CheckJob, len(jobs))
	resultChan := make(chan CheckResult, len(jobs))

	// Load jobs into channel
	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	var wg sync.WaitGroup
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}

	log.Debug().Msgf("Starting domain checks pool with %d workers for %d domains", concurrency, len(jobs))

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Error().Msgf("Recovered from panic in worker pool thread: %v", r)
				}
			}()

			for job := range jobChan {
				// Abort early if context is already cancelled
				if ctx.Err() != nil {
					resultChan <- CheckResult{
						NameID: job.NameID,
						Name:   job.Name,
						Domain: job.Domain,
						TLD:    job.TLD,
						Error:  ctx.Err(),
					}
					continue
				}

				// Check Redis Cache
				if wp.cacheSvc != nil {
					if cached, found := wp.cacheSvc.GetDomainCheck(ctx, job.Domain); found {
						resultChan <- CheckResult{
							NameID:    job.NameID,
							Name:      job.Name,
							Domain:    job.Domain,
							TLD:       job.TLD,
							Available: cached.Available,
							Price:     cached.Price,
							Currency:  cached.Currency,
							Platform:  cached.Platform,
							Offers:    cached.Offers,
							Cached:    true,
						}
						continue
					}
				}

				// Collapse identical domain queries using Singleflight
				val, err, _ := wp.sfGroup.Do(job.Domain, func() (interface{}, error) {
					res := wp.executeCheckWithRetry(ctx, job)
					return res, res.Error
				})

				res := val.(CheckResult)
				// Reinject context-specific fields
				res.NameID = job.NameID
				res.Name = job.Name
				res.TLD = job.TLD
				res.Error = err
				resultChan <- res
			}
		}()
	}

	wg.Wait()
	close(resultChan)

	results := make([]CheckResult, 0, len(jobs))
	for r := range resultChan {
		results = append(results, r)
	}

	return results
}

func isKeyless(prov providers.DomainProvider) bool {
	switch p := prov.(type) {
	case *providers.GoDaddyProvider:
		return p.APIKey == "" || p.APISecret == ""
	case *providers.HostingerProvider:
		return p.APIKey == ""
	case *providers.NamecheapProvider:
		return p.ApiKey == ""
	case *providers.PorkbunProvider:
		return p.APIKey == "" || p.SecretKey == ""
	case *providers.BigRockProvider:
		return p.APIKey == ""
	default:
		return false
	}
}

func (wp *WorkerPool) executeCheckWithRetry(ctx context.Context, job CheckJob) CheckResult {
	if len(wp.providers) == 0 {
		return CheckResult{
			NameID: job.NameID,
			Name:   job.Name,
			Domain: job.Domain,
			TLD:    job.TLD,
			Error:  errors.New("no providers registered"),
		}
	}

	var dRes *providers.DomainResult
	var err error
	var activeProv providers.DomainProvider

	// 1. Unified Availability Check (Fast Path)
	// Check if all providers are keyless.
	allKeyless := true
	for _, prov := range wp.providers {
		if !isKeyless(prov) {
			allKeyless = false
			break
		}
	}

	if allKeyless {
		// Keyless mode: perform a single unified availability check
		dRes, err = providers.KeylessCheckAvailability(ctx, nil, job.Domain)
		if err != nil {
			return CheckResult{
				NameID: job.NameID,
				Name:   job.Name,
				Domain: job.Domain,
				TLD:    job.TLD,
				Error:  err,
			}
		}
	} else {
		// Credential/Mock mode: run the failover provider loop with circuit-breaking checks
		for _, prov := range wp.providers {
			if wp.isCircuitBroken(prov) {
				continue
			}

			dRes, err = wp.checkAvailabilityWithProvider(ctx, prov, job.Domain)
			if err == nil {
				activeProv = prov
				break
			}

			wp.recordFailure(prov)
		}

		// If all configured providers fail/tripped, fallback to primary provider retry (best-effort)
		if err != nil || dRes == nil {
			log.Warn().Err(err).Msgf("All domain check providers failed or tripped for %s. Executing best-effort call on primary provider.", job.Domain)
			activeProv = wp.providers[0]
			wp.recordSuccess(activeProv) // Reset health metrics to attempt query
			dRes, err = wp.checkAvailabilityWithProvider(ctx, activeProv, job.Domain)
			if err != nil {
				return CheckResult{
					NameID: job.NameID,
					Name:   job.Name,
					Domain: job.Domain,
					TLD:    job.TLD,
					Error:  err,
				}
			}
		}
	}

	var offers []models.ProviderOffer
	var bestPrice float64
	var bestCurrency string
	var bestPlatform string

	// 2. Pricing Scan (Slow Path) - Only run if domain is available
	if dRes.Available {
		type priceResult struct {
			offers []models.ProviderOffer
			err    error
		}
		resChan := make(chan priceResult, len(wp.providers))
		var wg sync.WaitGroup

		for _, p := range wp.providers {
			if wp.isCircuitBroken(p) {
				continue
			}

			wg.Add(1)
			go func(prov providers.DomainProvider) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						log.Error().Msgf("Recovered from panic during pricing fetch: %v", r)
					}
				}()

				// Collapse identical concurrent pricing requests using priceSfGrp
				sfKey := fmt.Sprintf("%p_%s", prov, job.Domain)
				val, getErr, _ := wp.priceSfGrp.Do(sfKey, func() (interface{}, error) {
					var res *providers.PriceResult
					var innerErr error
					for attempt := 0; attempt < 2; attempt++ {
						if ctx.Err() != nil {
							return nil, ctx.Err()
						}

						res, innerErr = prov.GetPrice(ctx, job.Domain)
						if innerErr == nil {
							break
						}

						backoff := time.Duration((attempt+1)*50) * time.Millisecond
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-time.After(backoff):
						}
					}
					return res, innerErr
				})

				if getErr != nil {
					resChan <- priceResult{err: getErr}
					return
				}

				pRes := val.(*providers.PriceResult)

				var offersList []models.ProviderOffer
				if len(pRes.Plans) > 0 {
					for _, plan := range pRes.Plans {
						offersList = append(offersList, models.ProviderOffer{
							Platform: plan.Name,
							Price:    plan.Price,
							Currency: plan.Currency,
						})
					}
				} else {
					offersList = append(offersList, models.ProviderOffer{
						Platform: pRes.Platform,
						Price:    pRes.Price,
						Currency: pRes.Currency,
					})
				}

				resChan <- priceResult{
					offers: offersList,
				}
			}(p)
		}

		wg.Wait()
		close(resChan)

		for r := range resChan {
			if r.err == nil {
				offers = append(offers, r.offers...)
			}
		}

		if len(offers) == 0 {
			offers = append(offers, models.ProviderOffer{
				Platform: "Standard",
				Price:    12.00,
				Currency: "USD",
			})
		}

		cheapestIdx := -1
		lowestPriceInINR := -1.0

		// Find the cheapest standalone domain registration (exclude free/hosting bundles)
		for idx, off := range offers {
			platLower := strings.ToLower(off.Platform)
			if off.Price <= 0 || strings.Contains(platLower, "free with hosting") || strings.Contains(platLower, "hosting bundle") {
				continue
			}

			priceInINR := off.Price
			if off.Currency == "USD" {
				priceInINR = off.Price * 83.50
			}
			if lowestPriceInINR < 0 || priceInINR < lowestPriceInINR {
				lowestPriceInINR = priceInINR
				cheapestIdx = idx
			}
		}

		// Fallback if no standalone positive price offer was found
		if cheapestIdx == -1 {
			// Try to find the cheapest offer with price > 0 (even if it is bundled)
			for idx, off := range offers {
				if off.Price > 0 {
					priceInINR := off.Price
					if off.Currency == "USD" {
						priceInINR = off.Price * 83.50
					}
					if cheapestIdx == -1 || priceInINR < lowestPriceInINR {
						lowestPriceInINR = priceInINR
						cheapestIdx = idx
					}
				}
			}
			// Absolute fallback to first offer if everything is 0 or no positive price is found
			if cheapestIdx == -1 {
				cheapestIdx = 0
			}
		}

		offers[cheapestIdx].IsBest = true
		bestPrice = offers[cheapestIdx].Price
		bestCurrency = offers[cheapestIdx].Currency
		bestPlatform = offers[cheapestIdx].Platform
	} else {
		bestPrice = 0.00
		bestCurrency = "USD"
		bestPlatform = "Unavailable"
	}

	checkModel := &models.DomainCheck{
		Domain:    job.Domain,
		TLD:       job.TLD,
		Available: dRes.Available,
		Price:     bestPrice,
		Currency:  bestCurrency,
		Platform:  bestPlatform,
		Offers:    offers,
	}

	// Write cache asynchronously with safety recovery and context timeout
	if wp.cacheSvc != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Msgf("Recovered from panic during async cache set: %v", r)
				}
			}()
			cCtx, cCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cCancel()
			_ = wp.cacheSvc.SetDomainCheck(cCtx, job.Domain, checkModel, wp.cacheTTL)
		}()
	}

	return CheckResult{
		NameID:    job.NameID,
		Name:      job.Name,
		Domain:    job.Domain,
		TLD:       job.TLD,
		Available: dRes.Available,
		Price:     bestPrice,
		Currency:  bestCurrency,
		Platform:  bestPlatform,
		Offers:    offers,
		Cached:    false,
	}
}

func (wp *WorkerPool) checkAvailabilityWithProvider(ctx context.Context, prov providers.DomainProvider, domain string) (*providers.DomainResult, error) {
	var dRes *providers.DomainResult
	var err error

	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		wp.throttle(prov)
		dRes, err = prov.CheckAvailability(ctx, domain)
		if err == nil {
			wp.recordSuccess(prov)
			return dRes, nil
		}

		backoff := time.Duration((attempt+1)*50) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, err
}

func (wp *WorkerPool) throttle(prov providers.DomainProvider) {
	name := fmt.Sprintf("%p", prov)
	wp.throttleMu.Lock()
	tb, exists := wp.throttlers[name]
	if !exists {
		tb = newTokenBucket(100, 50) // Capacity 100, refilled 50 tokens/sec to optimize speed
		wp.throttlers[name] = tb
	}
	wp.throttleMu.Unlock()

	for {
		if tb.Take() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (wp *WorkerPool) isCircuitBroken(prov providers.DomainProvider) bool {
	name := fmt.Sprintf("%p", prov)
	wp.healthMu.Lock()
	defer wp.healthMu.Unlock()

	h, exists := wp.health[name]
	if !exists {
		return false
	}

	if h.tripped {
		if time.Now().After(h.trippedUntil) {
			h.tripped = false
			h.failures = 0
			return false
		}
		return true
	}
	return false
}

func (wp *WorkerPool) recordFailure(prov providers.DomainProvider) {
	name := fmt.Sprintf("%p", prov)
	wp.healthMu.Lock()
	defer wp.healthMu.Unlock()

	h, exists := wp.health[name]
	if !exists {
		h = &providerHealth{}
		wp.health[name] = h
	}

	h.failures++
	h.lastFailure = time.Now()
	if h.failures >= 5 {
		h.tripped = true
		h.trippedUntil = time.Now().Add(30 * time.Second)
		log.Warn().Msgf("Circuit breaker tripped for domain provider %s until %v", name, h.trippedUntil)
	}
}

func (wp *WorkerPool) recordSuccess(prov providers.DomainProvider) {
	name := fmt.Sprintf("%p", prov)
	wp.healthMu.Lock()
	defer wp.healthMu.Unlock()

	h, exists := wp.health[name]
	if exists {
		h.failures = 0
		h.tripped = false
	}
}

