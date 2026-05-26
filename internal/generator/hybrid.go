package generator

import (
	"math/rand"
	"strings"
	"time"

	"nameforge/internal/models"
)

type HybridGenerator struct{}

func NewHybridGenerator() *HybridGenerator {
	return &HybridGenerator{}
}

// Generate takes AI-generated names and recombines their syllables with morphological word banks.
func (h *HybridGenerator) Generate(req *models.GenerateRequest, aiNames []string) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano() + 2))
	namesSet := make(map[string]bool)

	if len(aiNames) == 0 {
		// Fallback: If no AI names are passed, construct mock AI names first
		fallbackGen := NewAIGenerator("")
		aiNames = fallbackGen.generateFallback(req)
	}

	// Extract syllables/roots from AI names
	var aiRoots []string
	for _, name := range aiNames {
		name = strings.ToLower(name)
		nameRunes := []rune(name)
		if len(nameRunes) <= 3 {
			aiRoots = append(aiRoots, name)
			continue
		}
		// Cut the word roughly in half (safe rune slicing)
		mid := len(nameRunes) / 2
		aiRoots = append(aiRoots, string(nameRunes[:mid]))
		aiRoots = append(aiRoots, string(nameRunes[mid:]))
	}

	// Seed options for recombination
	poolBases := append(physicsWords, natureWords...)
	poolBases = append(poolBases, customPrefix...)

	// Generate 40 names
	for i := 0; i < 40; i++ {
		// 50% chance: AI Root + Morphological Suffix
		// 50% chance: Morphological Base + AI Root (suffixified)
		var combined string
		if r.Float32() < 0.5 && len(aiRoots) > 0 {
			root := aiRoots[r.Intn(len(aiRoots))]
			suffix := customSuffix[r.Intn(len(customSuffix))]
			combined = smoothVowelsAndConsonants(root, suffix)
		} else if len(aiRoots) > 0 {
			base := poolBases[r.Intn(len(poolBases))]
			root := aiRoots[r.Intn(len(aiRoots))]
			// Apply truncation on base if too long (safe rune slicing)
			baseRunes := []rune(base)
			if len(baseRunes) > 4 {
				base = string(baseRunes[:4])
			}
			combined = smoothVowelsAndConsonants(base, root)
		} else {
			// Absolute backup
			base := poolBases[r.Intn(len(poolBases))]
			suffix := customSuffix[r.Intn(len(customSuffix))]
			combined = smoothVowelsAndConsonants(base, suffix)
		}

		capitalized := strings.Title(combined)
		namesSet[capitalized] = true
	}

	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}

	return names
}
