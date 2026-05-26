package generator

import (
	"context"
	"strings"
	"sync"

	"nameforge/internal/filters"
	"nameforge/internal/models"

	"github.com/rs/zerolog/log"
)

type Orchestrator struct {
	aiGen     *AIGenerator
	morphGen  *MorphologicalGenerator
	hybridGen *HybridGenerator
}

func NewOrchestrator(geminiKey string) *Orchestrator {
	return &Orchestrator{
		aiGen:     NewAIGenerator(geminiKey),
		morphGen:  NewMorphologicalGenerator(),
		hybridGen: NewHybridGenerator(),
	}
}

// GenerateNames runs all three naming engines and filters results to a clean list of brandable names.
func (o *Orchestrator) GenerateNames(ctx context.Context, req *models.GenerateRequest) []models.GeneratedName {
	var wg sync.WaitGroup
	var aiNames []string
	var morphNames []string
	var hybridNames []string

	// 1. AI Generation (can make network call, run in goroutine)
	wg.Add(1)
	go func() {
		defer wg.Done()
		names, err := o.aiGen.Generate(ctx, req)
		if err != nil {
			log.Error().Err(err).Msg("AI generation failed")
		} else {
			aiNames = names
		}
	}()

	// 2. Morphological Generation (purely CPU-bound, fast)
	wg.Add(1)
	go func() {
		defer wg.Done()
		morphNames = o.morphGen.Generate(req)
	}()

	wg.Wait()

	// 3. Hybrid Recombination (requires AI names to recombine, runs after AI completes)
	hybridNames = o.hybridGen.Generate(req, aiNames)

	// Merge all and run through filters
	type tempName struct {
		Name   string
		Source string
	}

	var candidates []tempName
	for _, n := range aiNames {
		candidates = append(candidates, tempName{Name: n, Source: "AI"})
	}
	for _, n := range morphNames {
		candidates = append(candidates, tempName{Name: n, Source: "Morphological"})
	}
	for _, n := range hybridNames {
		candidates = append(candidates, tempName{Name: n, Source: "Hybrid"})
	}

	uniqueNames := make(map[string]models.GeneratedName)
	for _, cand := range candidates {
		cleaned := strings.TrimSpace(cand.Name)
		if cleaned == "" {
			continue
		}

		// Ensure we don't repeat the exact name
		lowerKey := strings.ToLower(cleaned)
		if _, exists := uniqueNames[lowerKey]; exists {
			continue
		}

		// Evaluate name suitability using filter engine
		eval := filters.EvaluateName(cleaned, req.Avoid)
		if !eval.IsValid {
			log.Debug().Msgf("Filtering rejected name '%s' for reason: %s", cleaned, eval.Reason)
			continue
		}

		// Score from filters will be blended in ranking later
		brandScore := (eval.Pronounceability + eval.Memorability + eval.StartupFeel) / 3

		uniqueNames[lowerKey] = models.GeneratedName{
			Name:          cleaned,
			GeneratorType: cand.Source,
			Score:         brandScore,
		}
	}

	// Flatten map to slice
	results := make([]models.GeneratedName, 0, len(uniqueNames))
	for _, gn := range uniqueNames {
		results = append(results, gn)
	}

	log.Info().Msgf("Generated %d unique filtered name candidates (AI: %d, Morph: %d, Hybrid: %d)",
		len(results), len(aiNames), len(morphNames), len(hybridNames))

	return results
}
