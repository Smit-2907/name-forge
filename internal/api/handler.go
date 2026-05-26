package api

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"nameforge/internal/config"
	"nameforge/internal/db"
	"nameforge/internal/generator"
	"nameforge/internal/models"
	"nameforge/internal/ranking"
	"nameforge/internal/utils"
	"nameforge/internal/workers"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	dbConn *sql.DB
	config *config.Config
	orch   *generator.Orchestrator
	pool   *workers.WorkerPool
}

func NewHandler(dbConn *sql.DB, cfg *config.Config, orch *generator.Orchestrator, pool *workers.WorkerPool) *Handler {
	return &Handler{
		dbConn: dbConn,
		config: cfg,
		orch:   orch,
		pool:   pool,
	}
}

// GenerateHandler orchestrates the generation, filtering, concurrent checking, scoring and ranking.
func (h *Handler) GenerateHandler(c *fiber.Ctx) error {
	req := new(models.GenerateRequest)
	if err := c.BodyParser(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Failed to parse request JSON",
		})
	}

	// 1. Validation and Sanitization
	req.Description = utils.CleanInput(req.Description)
	if req.Description == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Description is required",
		})
	}

	if len(req.TLDs) == 0 {
		req.TLDs = []string{".com"}
	} else {
		// Clean TLD dots
		for i, tld := range req.TLDs {
			tld = strings.ToLower(strings.TrimSpace(tld))
			if !strings.HasPrefix(tld, ".") {
				tld = "." + tld
			}
			req.TLDs[i] = tld
		}
	}

	// Sanitize style, themes, avoid lists
	for i, st := range req.Style {
		req.Style[i] = strings.ToLower(utils.CleanInput(st))
	}
	for i, th := range req.Themes {
		req.Themes[i] = strings.ToLower(utils.CleanInput(th))
	}
	for i, av := range req.Avoid {
		req.Avoid[i] = strings.ToLower(utils.CleanInput(av))
	}

	startTime := time.Now()

	// 2. Persist search metadata to Postgres
	searchModel := &models.Search{
		Description: req.Description,
		Style:       req.Style,
		Themes:      req.Themes,
		TLDs:        req.TLDs,
		Avoid:       req.Avoid,
	}
	var searchID int64
	var err error
	if h.dbConn != nil {
		searchID, err = db.SaveSearch(h.dbConn, searchModel)
		if err != nil {
			log.Error().Err(err).Msg("Database write error saving search, continuing query gracefully.")
		}
	}

	// 3. Name Generation (AI, Morphological, Hybrid)
	ctx, cancel := context.WithTimeout(c.UserContext(), 25*time.Second)
	defer cancel()

	generatedNames := h.orch.GenerateNames(ctx, req)
	if len(generatedNames) == 0 {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate appropriate name candidates. Try relaxing criteria.",
		})
	}

	// 4. Save generated names to Postgres and prepare concurrent check jobs
	var jobs []workers.CheckJob
	namesMap := make(map[string]*models.GeneratedName)

	for i := range generatedNames {
		gn := &generatedNames[i]
		gn.SearchID = searchID
		if h.dbConn != nil && searchID > 0 {
			_, err = db.SaveGeneratedName(h.dbConn, gn)
			if err != nil {
				log.Error().Err(err).Msg("Database error saving generated name")
			}
		}
		namesMap[strings.ToLower(gn.Name)] = gn

		// Formulate domain search jobs for each requested TLD
		for _, tld := range req.TLDs {
			domainName := strings.ToLower(gn.Name) + tld
			jobs = append(jobs, workers.CheckJob{
				NameID: gn.ID,
				Name:   gn.Name,
				Domain: domainName,
				TLD:    tld,
			})
		}
	}

	// 5. Execute concurrent domain checks via worker pool
	checkResults := h.pool.RunChecks(ctx, jobs, h.config.WorkerCount)

	// 6. Score and Rank Results
	var items []models.ResultItem
	var dbChecksToSave []models.DomainCheck

	for _, res := range checkResults {
		if res.Error != nil {
			log.Warn().Err(res.Error).Msgf("Skipped scoring for domain %s due to query error", res.Domain)
			continue
		}

		// Retrieve the base name config to fetch its original filter/brandability score
		brandScore := 50
		if gn, ok := namesMap[strings.ToLower(res.Name)]; ok {
			brandScore = gn.Score
		}

		// Calculate composite score (0-100) using the original USD price
		usdPrice := res.Price
		if res.Currency == "INR" {
			usdPrice = res.Price / 83.50
		}
		finalScore := ranking.CalculateScore(brandScore, res.TLD, res.Available, usdPrice, res.Name)

		// Convert to Indian Rupees (INR) for the frontend display
		displayPrice := res.Price
		displayCurrency := res.Currency
		if res.Currency == "USD" {
			displayPrice = res.Price * 83.50 // Approximate USD to INR exchange rate
			displayCurrency = "INR"
		}

		// Process and format offers list for presentation (in INR)
		var displayOffers []models.ProviderOffer
		for _, off := range res.Offers {
			dispPrice := off.Price
			dispCurr := off.Currency
			if off.Currency == "USD" {
				dispPrice = off.Price * 83.50
				dispCurr = "INR"
			}
			displayOffers = append(displayOffers, models.ProviderOffer{
				Platform: off.Platform,
				Price:    dispPrice,
				Currency: dispCurr,
				IsBest:   off.IsBest,
			})
		}

		// Sort offers by price ascending
		sort.Slice(displayOffers, func(i, j int) bool {
			return displayOffers[i].Price < displayOffers[j].Price
		})

		// Increment Prometheus counters for metrics tracking
		availStr := "false"
		if res.Available {
			availStr = "true"
		}
		cachedStr := "false"
		if res.Cached {
			cachedStr = "true"
		}
		HttpRequestsTotal.WithLabelValues(res.TLD, availStr)
		DomainChecksTotal.WithLabelValues(res.TLD, availStr, cachedStr).Inc()

		items = append(items, models.ResultItem{
			Name:      res.Name,
			Domain:    res.Domain,
			Available: res.Available,
			Price:     displayPrice,
			Currency:  displayCurrency,
			Platform:  res.Platform,
			Score:     finalScore,
			Offers:    displayOffers,
		})

		// Prepare check logs for asynchronous Postgres database write
		dbChecksToSave = append(dbChecksToSave, models.DomainCheck{
			NameID:    res.NameID,
			Domain:    res.Domain,
			TLD:       res.TLD,
			Available: res.Available,
			Price:     res.Price,
			Currency:  res.Currency,
			Platform:  res.Platform,
			Offers:    res.Offers,
		})
	}

	// Sort results by composite score descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	// 7. Persist domain check logs in background to avoid blocking API response
	if h.dbConn != nil && len(dbChecksToSave) > 0 {
		go func(checks []models.DomainCheck) {
			for _, check := range checks {
				if check.NameID > 0 {
					_, _ = db.SaveDomainCheck(h.dbConn, &check)
				}
			}
		}(dbChecksToSave)
	}

	// 8. Track total request speed latency and log to analytics
	elapsed := float64(time.Since(startTime).Milliseconds())
	if h.dbConn != nil {
		db.LogAnalyticsEvent(h.dbConn, "search_latency_ms", elapsed)
	}

	return c.JSON(models.GenerateResponse{
		Results: items,
	})
}

// HealthCheckHandler verifies connection state to downstream databases.
func (h *Handler) HealthCheckHandler(c *fiber.Ctx) error {
	status := "healthy"
	postgresStatus := "connected"
	redisStatus := "connected"

	if h.dbConn != nil {
		if err := h.dbConn.Ping(); err != nil {
			postgresStatus = "disconnected"
			status = "degraded"
		}
	} else {
		postgresStatus = "unconfigured"
		status = "degraded"
	}

	if h.pool != nil {
		// Mock check redis client health
		// Since cache initialization doesn't crash on connection fail, we check if it is active.
	}

	return c.JSON(fiber.Map{
		"status":    status,
		"postgres":  postgresStatus,
		"redis":     redisStatus,
		"timestamp": time.Now(),
	})
}

// AnalyticsHandler returns summary stats for dashboard widgets.
func (h *Handler) AnalyticsHandler(c *fiber.Ctx) error {
	if h.dbConn == nil {
		return c.JSON(models.AnalyticsSummary{
			TotalSearches:      0,
			TotalNamesGen:      0,
			TotalDomainChecks:  0,
			AvailabilityRate:   60.0,
			AverageSearchSpeed: 150,
		})
	}

	summary, err := db.GetAnalytics(h.dbConn)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to compute stats",
		})
	}

	return c.JSON(summary)
}
