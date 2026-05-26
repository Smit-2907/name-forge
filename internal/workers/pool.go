package workers

import (
	"context"
	"errors"
	"sync"
	"time"

	"nameforge/internal/cache"
	"nameforge/internal/models"
	"nameforge/internal/providers"

	"github.com/rs/zerolog/log"
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

// WorkerPool handles concurrent execution of domain check jobs.
type WorkerPool struct {
	providers []providers.DomainProvider
	cacheSvc  *cache.CacheService
	cacheTTL  time.Duration
}

func NewWorkerPool(providers []providers.DomainProvider, cacheSvc *cache.CacheService, cacheTTL time.Duration) *WorkerPool {
	return &WorkerPool{
		providers: providers,
		cacheSvc:  cacheSvc,
		cacheTTL:  cacheTTL,
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
	// Limit concurrency to size of jobs if jobs are fewer than configured workers
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}

	log.Debug().Msgf("Starting domain checks pool with %d workers for %d domains", concurrency, len(jobs))

	// Spawn worker goroutines
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
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

				// Query Provider with rate limits/retry protection
				res := wp.executeCheckWithRetry(ctx, job)
				resultChan <- res
			}
		}(i)
	}

	// Wait for workers to finish and close results channel
	wg.Wait()
	close(resultChan)

	// Collect results
	results := make([]CheckResult, 0, len(jobs))
	for r := range resultChan {
		results = append(results, r)
	}

	return results
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

	// We only need to check availability on ONE provider (availability is global)
	primaryProvider := wp.providers[0]

	// Retry loop for checking availability (up to 3 attempts with exponential backoff)
	for attempt := 0; attempt < 3; attempt++ {
		// Respect context timeout/cancellation during retries
		if ctx.Err() != nil {
			return CheckResult{
				NameID: job.NameID,
				Name:   job.Name,
				Domain: job.Domain,
				TLD:    job.TLD,
				Error:  ctx.Err(),
			}
		}

		dRes, err = primaryProvider.CheckAvailability(ctx, job.Domain)
		if err == nil {
			break
		}

		backoff := time.Duration((attempt+1)*50) * time.Millisecond
		log.Warn().Err(err).Msgf("Worker availability check failed for %s. Retrying in %v...", job.Domain, backoff)
		time.Sleep(backoff)
	}

	if err != nil {
		return CheckResult{
			NameID: job.NameID,
			Name:   job.Name,
			Domain: job.Domain,
			TLD:    job.TLD,
			Error:  err,
		}
	}

	var offers []models.ProviderOffer
	var bestPrice float64
	var bestCurrency string
	var bestPlatform string

	if dRes.Available {
		type priceResult struct {
			offer models.ProviderOffer
			err   error
		}
		resChan := make(chan priceResult, len(wp.providers))
		var wg sync.WaitGroup

		for _, p := range wp.providers {
			wg.Add(1)
			go func(prov providers.DomainProvider) {
				defer wg.Done()
				var pRes *providers.PriceResult
				var pErr error
				for attempt := 0; attempt < 2; attempt++ {
					pRes, pErr = prov.GetPrice(ctx, job.Domain)
					if pErr == nil {
						break
					}
					time.Sleep(time.Duration((attempt+1)*50) * time.Millisecond)
				}

				if pErr != nil {
					resChan <- priceResult{err: pErr}
					return
				}

				resChan <- priceResult{
					offer: models.ProviderOffer{
						Platform: pRes.Platform,
						Price:    pRes.Price,
						Currency: pRes.Currency,
					},
				}
			}(p)
		}

		wg.Wait()
		close(resChan)

		// Collect results
		for r := range resChan {
			if r.err == nil {
				offers = append(offers, r.offer)
			}
		}

		// Fallback pricing if all provider pricing calls failed
		if len(offers) == 0 {
			offers = append(offers, models.ProviderOffer{
				Platform: "Standard",
				Price:    12.00,
				Currency: "USD",
			})
		}

		// Determine the best deal (cheapest offer)
		// We convert price values to INR for the comparison
		cheapestIdx := 0
		lowestPriceInINR := -1.0

		for idx, off := range offers {
			priceInINR := off.Price
			if off.Currency == "USD" {
				priceInINR = off.Price * 83.50
			}
			if lowestPriceInINR < 0 || priceInINR < lowestPriceInINR {
				lowestPriceInINR = priceInINR
				cheapestIdx = idx
			}
		}

		// Mark the cheapest offer
		offers[cheapestIdx].IsBest = true

		bestPrice = offers[cheapestIdx].Price
		bestCurrency = offers[cheapestIdx].Currency
		bestPlatform = offers[cheapestIdx].Platform
	} else {
		bestPrice = 0.00
		bestCurrency = "USD"
		bestPlatform = "Unavailable"
	}

	// Write to Redis cache asynchronously
	if wp.cacheSvc != nil {
		checkModel := &models.DomainCheck{
			Domain:    job.Domain,
			TLD:       job.TLD,
			Available: dRes.Available,
			Price:     bestPrice,
			Currency:  bestCurrency,
			Platform:  bestPlatform,
			Offers:    offers,
		}
		go func() {
			_ = wp.cacheSvc.SetDomainCheck(context.Background(), job.Domain, checkModel, wp.cacheTTL)
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
