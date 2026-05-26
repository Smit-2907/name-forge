package ranking

import (
	"strings"
)

// CalculateScore computes a composite brand score from 0 to 100.
func CalculateScore(brandScore int, tld string, available bool, price float64, name string) int {
	score := brandScore

	// 1. Availability Multiplier / Penalty
	if !available {
		score -= 45 // Severe penalty for taken domains
	} else {
		score += 10 // Bonus for availability
	}

	// 2. TLD Priority Boost
	tldLower := strings.ToLower(tld)
	if !strings.HasPrefix(tldLower, ".") {
		tldLower = "." + tldLower
	}

	switch tldLower {
	case ".com":
		score += 15 // Ultimate gold standard
	case ".ai":
		score += 12 // Premium AI boost
	case ".io":
		score += 8 // Developer/Tech boost
	case ".net":
		score += 3
	}

	// 3. Length Preference
	nameLen := len(name)
	if nameLen >= 4 && nameLen <= 6 {
		score += 10 // Ideal brand name length
	} else if nameLen <= 8 {
		score += 5
	} else if nameLen > 11 {
		score -= 10 // Harder to remember
	}

	// 4. Price Scoring (Only relevant if available; if not available price is 0)
	if available {
		if price <= 10.00 {
			score += 10 // Bargain price
		} else if price <= 20.00 {
			score += 5 // Standard registration price
		} else if price > 100.00 && price <= 500.00 {
			score -= 10 // Premium domain pricing
		} else if price > 500.00 {
			score -= 25 // Very expensive
		}
	}

	// Clamp the score between 1 and 100
	if score > 100 {
		return 100
	}
	if score < 1 {
		return 1
	}
	return score
}
