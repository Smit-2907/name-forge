package generator

import (
	"math/rand"
	"strings"
	"time"

	"nameforge/internal/models"
)

var (
	physicsWords = []string{"quantum", "flux", "vector", "orbit", "lumen", "pulse", "nova", "kinetic", "vertex", "electron", "wave", "matrix", "spectra", "laser"}
	natureWords  = []string{"pine", "mist", "river", "stone", "forest", "peak", "aurora", "cloud", "canyon", "solar", "ocean", "valley", "ridge", "wind", "leaf"}
	customPrefix = []string{"vel", "nov", "aur", "flu", "lum", "vec", "orb", "kin", "dyn", "syn", "neo", "zen", "flex", "omn", "hyper"}
	customSuffix = []string{"ora", "en", "is", "ex", "alis", "aryn", "ica", "on", "ux", "ent", "as", "us", "io", "ify", "ix"}
)

type MorphologicalGenerator struct{}

func NewMorphologicalGenerator() *MorphologicalGenerator {
	return &MorphologicalGenerator{}
}

func (m *MorphologicalGenerator) Generate(req *models.GenerateRequest) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano() + 1))
	namesSet := make(map[string]bool)

	// Determine which base list to prioritize based on themes
	var bases []string
	hasPhysics := false
	hasNature := false
	for _, theme := range req.Themes {
		themeLower := strings.ToLower(theme)
		if themeLower == "physics" {
			hasPhysics = true
		} else if themeLower == "nature" {
			hasNature = true
		}
	}

	if hasPhysics {
		bases = append(bases, physicsWords...)
	}
	if hasNature {
		bases = append(bases, natureWords...)
	}
	if len(bases) == 0 {
		// Use a mix if no specific theme matched
		bases = append(bases, physicsWords...)
		bases = append(bases, natureWords...)
	}

	// Always allow some prefix-based generation
	bases = append(bases, customPrefix...)

	// Generate 40 names
	for i := 0; i < 40; i++ {
		base := bases[r.Intn(len(bases))]
		suffix := customSuffix[r.Intn(len(customSuffix))]

		// Apply Truncation on base sometimes (50% chance)
		if r.Float32() < 0.5 && len(base) > 4 {
			base = truncateWord(base, r.Intn(2)+3) // Keep 3 to 4 characters
		}

		combined := smoothVowelsAndConsonants(base, suffix)
		capitalized := strings.Title(combined)
		namesSet[capitalized] = true
	}

	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}

	return names
}

// truncateWord keeps the first N chars of a word.
func truncateWord(word string, length int) string {
	if len(word) <= length {
		return word
	}
	return word[:length]
}

// smoothVowelsAndConsonants applies vowel smoothing and handles consonant clashes.
func smoothVowelsAndConsonants(base, suffix string) string {
	isVowel := func(b byte) bool {
		return strings.ContainsAny(string(b), "aeiouyAEIOUY")
	}

	if len(base) == 0 {
		return suffix
	}
	if len(suffix) == 0 {
		return base
	}

	lastCharBase := base[len(base)-1]
	firstCharSuffix := suffix[0]

	// 1. Vowel Smoothing: if both are vowels, drop one (usually base's last)
	if isVowel(lastCharBase) && isVowel(firstCharSuffix) {
		return base[:len(base)-1] + suffix
	}

	// 2. Consonant Clash: if both are consonants, insert a smoothing vowel (e, i, o) in 70% of cases
	if !isVowel(lastCharBase) && !isVowel(firstCharSuffix) {
		// Exceptions: e.g. "xt" or "st" are fine.
		clashExceptions := map[string]bool{
			"st": true, "rt": true, "nd": true, "nt": true, "tr": true, "fl": true, "gr": true, "pr": true,
		}
		pair := string(lastCharBase) + string(firstCharSuffix)
		if !clashExceptions[pair] {
			smoothingVowels := []string{"e", "i", "o", "a"}
			// seeded random isn't strictly needed for a quick index pick
			idx := int(lastCharBase+firstCharSuffix) % len(smoothingVowels)
			return base + smoothingVowels[idx] + suffix
		}
	}

	return base + suffix
}
