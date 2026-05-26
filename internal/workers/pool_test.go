package workers

import (
	"context"
	"testing"
	"time"

	"nameforge/internal/providers"
)

func TestWorkerPool_RunChecks(t *testing.T) {
	mockProv := providers.NewMockProvider("Mock", "USD", 9.99, 4.99, 59.99, 34.99)
	pool := NewWorkerPool([]providers.DomainProvider{mockProv}, nil, 24*time.Hour)

	jobs := []CheckJob{
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Veltrix", Domain: "veltrix.ai", TLD: ".ai"},
		{Name: "Novaryn", Domain: "novaryn.com", TLD: ".com"},
		{Name: "Auralis", Domain: "auralis.io", TLD: ".io"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results := pool.RunChecks(ctx, jobs, 4)

	if len(results) != len(jobs) {
		t.Fatalf("Expected %d results, got %d", len(jobs), len(results))
	}

	for _, res := range results {
		if res.Error != nil {
			t.Errorf("Job for domain %s failed: %v", res.Domain, res.Error)
		}
		if res.Domain == "" {
			t.Error("Result domain is empty")
		}
	}
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	mockProv := providers.NewMockProvider("Mock", "USD", 9.99, 4.99, 59.99, 34.99)
	pool := NewWorkerPool([]providers.DomainProvider{mockProv}, nil, 24*time.Hour)

	jobs := []CheckJob{
		{Name: "Veltrix", Domain: "veltrix.com", TLD: ".com"},
		{Name: "Novaryn", Domain: "novaryn.com", TLD: ".com"},
	}

	// Immediate cancellation context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel upfront

	results := pool.RunChecks(ctx, jobs, 2)

	for _, res := range results {
		if res.Error != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", res.Error)
		}
	}
}
