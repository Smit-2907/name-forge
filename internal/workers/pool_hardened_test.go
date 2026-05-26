package workers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"nameforge/internal/providers"
)

type trackingMockProvider struct {
	providers.DomainProvider
	mu        sync.Mutex
	callCount int
	fail      bool
}

func (t *trackingMockProvider) CheckAvailability(ctx context.Context, domain string) (*providers.DomainResult, error) {
	t.mu.Lock()
	t.callCount++
	fail := t.fail
	t.mu.Unlock()

	// Short sleep to allow concurrency overlapping for single-flight tests
	time.Sleep(50 * time.Millisecond)

	if fail {
		return nil, errors.New("simulated error")
	}

	return &providers.DomainResult{Domain: domain, Available: true}, nil
}

func (t *trackingMockProvider) GetPrice(ctx context.Context, domain string) (*providers.PriceResult, error) {
	return &providers.PriceResult{Price: 10.0, Currency: "USD", Platform: "TrackingMock"}, nil
}

func TestWorkerPool_SingleFlightCollapsing(t *testing.T) {
	tracker := &trackingMockProvider{}
	pool := NewWorkerPool([]providers.DomainProvider{tracker}, nil, 24*time.Hour)

	// Run multiple identical jobs concurrently
	jobs := []CheckJob{
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results := pool.RunChecks(ctx, jobs, 5)

	if len(results) != 5 {
		t.Fatalf("Expected 5 results, got %d", len(results))
	}

	tracker.mu.Lock()
	calls := tracker.callCount
	tracker.mu.Unlock()

	// Without singleflight collapsing, the provider would be called 5 times.
	// With singleflight collapsing, it should be called exactly 1 time.
	if calls != 1 {
		t.Errorf("Singleflight failed: expected 1 call, got %d", calls)
	}
}

func TestWorkerPool_CircuitBreaker(t *testing.T) {
	tracker := &trackingMockProvider{fail: true}
	// Add fallback provider
	fallback := &trackingMockProvider{fail: false}

	pool := NewWorkerPool([]providers.DomainProvider{tracker, fallback}, nil, 24*time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initial checks that fail
	for i := 0; i < 5; i++ {
		job := CheckJob{Name: "Test", Domain: fmt.Sprintf("test%d.com", i), TLD: ".com"}
		// Execute checks
		_ = pool.RunChecks(ctx, []CheckJob{job}, 1)
	}

	// Tracker should have been called 5 times and failed
	tracker.mu.Lock()
	failuresRecorded := tracker.callCount
	tracker.mu.Unlock()

	if failuresRecorded != 15 {
		t.Fatalf("Expected 15 initial failure calls on primary, got %d", failuresRecorded)
	}

	// At this point, circuit breaker for tracker should be TRIPPED.
	// A new check should bypass tracker and route directly to fallback
	job := CheckJob{Name: "BypassTest", Domain: "bypasstest.com", TLD: ".com"}
	results := pool.RunChecks(ctx, []CheckJob{job}, 1)

	if len(results) != 1 || results[0].Error != nil {
		t.Errorf("Expected check to succeed via fallback, got error: %v", results[0].Error)
	}

	// Verify primary tracker was not called again (tripped)
	tracker.mu.Lock()
	callsAfterTrip := tracker.callCount
	tracker.mu.Unlock()

	if callsAfterTrip != 15 {
		t.Errorf("Circuit breaker failed: expected calls on primary to remain at 15, got %d", callsAfterTrip)
	}

	// Verify fallback was called
	fallback.mu.Lock()
	fallbackCalls := fallback.callCount
	fallback.mu.Unlock()

	if fallbackCalls != 6 {
		t.Errorf("Fallback provider was not used correctly: expected 6 calls, got %d", fallbackCalls)
	}
}
