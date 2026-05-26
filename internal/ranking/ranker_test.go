package ranking

import (
	"testing"
)

func TestCalculateScore(t *testing.T) {
	// Base brand score = 80
	// 1. Available .com name with standard price
	score1 := CalculateScore(80, ".com", true, 9.99, "veltrix")
	// Expected: 80 + 10 (avail) + 15 (.com) + 5 (len 7) + 10 (cheap price) = 120 (capped at 100)
	if score1 != 100 {
		t.Errorf("Expected score1 to be 100, got %d", score1)
	}

	// 2. Taken name
	score2 := CalculateScore(80, ".com", false, 0, "veltrix")
	// Expected: 80 - 45 (taken penalty) + 15 (.com) + 5 (len 7) = 55
	if score2 != 55 {
		t.Errorf("Expected score2 to be 55, got %d", score2)
	}

	// 3. Expensive premium domain
	score3 := CalculateScore(80, ".io", true, 800.00, "veltrix")
	// Expected: 80 + 10 (avail) + 8 (.io) + 5 (len 7) - 25 (expensive) = 78
	if score3 != 78 {
		t.Errorf("Expected score3 to be 78, got %d", score3)
	}
}
