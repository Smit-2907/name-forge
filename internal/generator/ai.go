package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nameforge/internal/models"

	"github.com/rs/zerolog/log"
)

type AIGenerator struct {
	APIKey string
	Client *http.Client
}

func NewAIGenerator(apiKey string) *AIGenerator {
	return &AIGenerator{
		APIKey: apiKey,
		Client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

// Generate uses Gemini API or falls back to template-based generation if no API key exists.
func (a *AIGenerator) Generate(ctx context.Context, req *models.GenerateRequest) ([]string, error) {
	if a.APIKey == "" {
		log.Info().Msg("Gemini API key not set, using smart fallback local generator.")
		return a.generateFallback(req), nil
	}

	// Escape double quotes to prevent prompt template hijacking
	escapedDesc := strings.ReplaceAll(req.Description, `"`, `\"`)

	prompt := fmt.Sprintf(`You are a world-class startup naming consultant.
Generate 40 short, highly pronounceable, brandable startup names for the following business.
Business Description: "%s"
Preferred Style/Vibe: %s
Themes to draw inspiration from: %s
Keywords to absolutely avoid: %s

Rules:
1. MUST be short (4-10 characters).
2. MUST be pronounceable and premium.
3. Keep it as single words (e.g. Veltrix, Auralis, Novaryn).
4. No hyphens or spaces.
5. Return ONLY a comma-separated list of names. No intro, no explanation, no bullet points.
`, escapedDesc, strings.Join(req.Style, ", "), strings.Join(req.Themes, ", "), strings.Join(req.Avoid, ", "))

	body := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=%s", a.APIKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(httpReq)
	if err != nil {
		log.Warn().Err(err).Msg("Gemini call failed, falling back to local generator.")
		return a.generateFallback(req), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Msgf("Gemini API returned non-OK status: %d. Falling back to local generator.", resp.StatusCode)
		return a.generateFallback(req), nil
	}

	var res geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return a.generateFallback(req), nil
	}

	content := res.Candidates[0].Content.Parts[0].Text

	// Defensive parsing: split by commas or newlines
	content = strings.ReplaceAll(content, "\r", "")
	var rawItems []string
	if strings.Contains(content, ",") {
		rawItems = strings.Split(content, ",")
	} else {
		rawItems = strings.Split(content, "\n")
	}

	names := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		cleaned := strings.TrimSpace(item)
		// Clean markdown bullet points
		cleaned = strings.TrimPrefix(cleaned, "*")
		cleaned = strings.TrimPrefix(cleaned, "-")
		cleaned = strings.TrimSpace(cleaned)

		// Remove numbered prefixes (e.g., "1. Name" -> "Name")
		if idx := strings.Index(cleaned, "."); idx > 0 && idx < 4 {
			numPrefix := cleaned[:idx]
			if _, err := strconv.Atoi(numPrefix); err == nil {
				cleaned = strings.TrimSpace(cleaned[idx+1:])
			}
		}

		if cleaned != "" {
			names = append(names, cleaned)
		}
	}

	return names, nil
}

// generateFallback uses a local rule-based system to match user descriptions and themes with premium syllables.
func (a *AIGenerator) generateFallback(req *models.GenerateRequest) []string {
	// Root bases matched to common startup tech areas
	techRoots := []string{"sys", "ops", "bpo", "flow", "core", "net", "data", "bot", "ai", "node", "grid", "wire", "link", "stack", "byte"}
	intelRoots := []string{"cog", "mind", "intel", "sage", "think", "brain", "know", "smart", "ai", "neuron", "opt"}
	financeRoots := []string{"pay", "coin", "ledger", "mint", "cap", "yield", "vest", "vault", "fund", "cred"}

	descLower := strings.ToLower(req.Description)

	var bases []string
	if strings.Contains(descLower, "support") || strings.Contains(descLower, "agent") || strings.Contains(descLower, "bpo") || strings.Contains(descLower, "automation") {
		bases = append(bases, "agent", "bpo", "chat", "desk", "help", "auto")
	}
	if strings.Contains(descLower, "ai") || strings.Contains(descLower, "intelligence") || strings.Contains(descLower, "smart") {
		bases = append(bases, intelRoots...)
	} else if strings.Contains(descLower, "finance") || strings.Contains(descLower, "crypto") || strings.Contains(descLower, "pay") {
		bases = append(bases, financeRoots...)
	} else {
		bases = append(bases, techRoots...)
	}

	// Mix in user themes
	for _, theme := range req.Themes {
		theme = strings.ToLower(theme)
		if theme == "physics" {
			bases = append(bases, "atom", "gluon", "spin", "field", "wave", "ray")
		} else if theme == "nature" {
			bases = append(bases, "leaf", "eco", "root", "grow", "soil", "wind")
		} else {
			if len(theme) > 2 {
				bases = append(bases, theme)
			}
		}
	}

	// Premium styling suffixes
	premiumSuffixes := []string{"ora", "ix", "ly", "ify", "io", "is", "a", "us", "ex", "va", "on", "ux", "ax", "zen", "ist"}

	// Generate combinations
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	namesSet := make(map[string]bool)

	for i := 0; i < 40; i++ {
		base := bases[r.Intn(len(bases))]
		suffix := premiumSuffixes[r.Intn(len(premiumSuffixes))]

		// Capitalize nicely
		name := strings.Title(base + suffix)
		namesSet[name] = true
	}

	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}

	return names
}
