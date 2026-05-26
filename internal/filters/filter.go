package filters

import (
	"regexp"
	"strings"
)

type FilterResult struct {
	IsValid            bool
	Reason             string
	Pronounceability   int // 0-100
	Memorability       int // 0-100
	StartupFeel        int // 0-100
	DomainFriendliness int // 0-100
}

var (
	consonantsReg = regexp.MustCompile(`(?i)[bcdfghjklmnpqrstvwxyz]{4,}`)
	vowelsRegEx   = regexp.MustCompile(`(?i)[aeiouy]{4,}`)
)

// EvaluateName runs filters and computes brandability scores.
func EvaluateName(name string, avoidWords []string) *FilterResult {
	cleaned := strings.TrimSpace(name)
	lower := strings.ToLower(cleaned)

	// Rule 1: Length constraints
	if len(cleaned) < 3 {
		return &FilterResult{IsValid: false, Reason: "Too short"}
	}
	if len(cleaned) > 15 {
		return &FilterResult{IsValid: false, Reason: "Too long"}
	}

	// Rule 2: Non-alphabetic characters (allow only pure alphabetic for brandable startup names)
	if match, _ := regexp.MatchString(`[^a-zA-Z]`, cleaned); match {
		return &FilterResult{IsValid: false, Reason: "Contains non-alphabetic characters"}
	}

	// Rule 3: Avoid keywords matching
	for _, av := range avoidWords {
		if av != "" && strings.Contains(lower, strings.ToLower(av)) {
			return &FilterResult{IsValid: false, Reason: "Matches avoided keyword: " + av}
		}
	}

	// Rule 4: Excessive consonants (e.g. "strngh")
	if consonantsReg.MatchString(cleaned) {
		return &FilterResult{IsValid: false, Reason: "Excessive consecutive consonants"}
	}

	// Rule 5: Excessive vowels (e.g. "queeei")
	if vowelsRegEx.MatchString(cleaned) {
		return &FilterResult{IsValid: false, Reason: "Excessive consecutive vowels"}
	}

	// Rule 6: Repeated characters (e.g. "abbby")
	if hasTripleRepeatedChar(cleaned) {
		return &FilterResult{IsValid: false, Reason: "Character repeated 3+ times"}
	}

	// Compute brandability metrics for names that pass basic checks
	pScore := calculatePronounceability(lower)
	mScore := calculateMemorability(lower)
	sScore := calculateStartupFeel(lower)
	dScore := calculateDomainFriendliness(lower)

	return &FilterResult{
		IsValid:            true,
		Pronounceability:   pScore,
		Memorability:       mScore,
		StartupFeel:        sScore,
		DomainFriendliness: dScore,
	}
}

// calculatePronounceability scores based on consonant-vowel transitions.
// Natural alternating patterns (e.g., CVCV, VCVC) get highest scores.
func calculatePronounceability(name string) int {
	isVowel := func(c rune) bool {
		return strings.ContainsRune("aeiouy", c)
	}

	runes := []rune(name)
	if len(runes) == 0 {
		return 0
	}

	transitions := 0
	for i := 0; i < len(runes)-1; i++ {
		if isVowel(runes[i]) != isVowel(runes[i+1]) {
			transitions++
		}
	}

	// Perfect score would be transition at every step.
	// We normalize transitions relative to max possible transitions (len - 1).
	maxTransitions := len(runes) - 1
	if maxTransitions == 0 {
		return 50
	}

	ratio := float64(transitions) / float64(maxTransitions)
	score := int(ratio * 100)

	// Boost for ending in a clean vowel sound or smooth consonant
	if strings.HasSuffix(name, "a") || strings.HasSuffix(name, "o") || strings.HasSuffix(name, "e") || strings.HasSuffix(name, "x") {
		score += 10
	}

	if score > 100 {
		return 100
	}
	if score < 20 {
		return 20 // baseline
	}
	return score
}

// calculateMemorability scores based on word length. Shorter is easier to remember.
func calculateMemorability(name string) int {
	length := len(name)
	switch {
	case length <= 5:
		return 95 // 4-5 letter names are gold standard
	case length <= 7:
		return 85
	case length <= 10:
		return 70
	case length <= 12:
		return 50
	default:
		return 30
	}
}

// calculateStartupFeel checks for common premium startup prefixes/suffixes.
func calculateStartupFeel(name string) int {
	score := 60 // Baseline

	// High value suffixes
	suffixes := []string{"ix", "ly", "ify", "io", "ora", "is", "ex", "va", "us", "on"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			score += 25
			break
		}
	}

	// High value prefixes
	prefixes := []string{"vel", "nov", "neo", "syn", "vol", "zen", "flex", "omn"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			score += 15
			break
		}
	}

	if score > 100 {
		return 100
	}
	return score
}

// calculateDomainFriendliness scores based on simplicity, lack of hyphens, double letters.
func calculateDomainFriendliness(name string) int {
	score := 90

	// Deduct for double letters (e.g. "ss", "ll")
	runes := []rune(name)
	doubleLetters := 0
	for i := 0; i < len(runes)-1; i++ {
		if runes[i] == runes[i+1] {
			doubleLetters++
		}
	}
	score -= doubleLetters * 15

	// Shorter names are easier to type
	score -= (len(name) - 4) * 3

	if score > 100 {
		return 100
	}
	if score < 10 {
		return 10
	}
	return score
}

func hasTripleRepeatedChar(s string) bool {
	runes := []rune(strings.ToLower(s))
	for i := 0; i < len(runes)-2; i++ {
		if runes[i] == runes[i+1] && runes[i] == runes[i+2] {
			return true
		}
	}
	return false
}
