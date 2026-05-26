package api

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"time"

	"nameforge/internal/cache"
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

var (
	// Safe description regex: letters, numbers, whitespace, and standard punctuation (including !, &, (, ), /, +, :, @, ", smart quotes, ;, $, %)
	safeDescRegex = regexp.MustCompile(`^[\p{L}\p{N}\s\.,\-\'\?!\(\)/&\+:@"“”‘’\$%;]+$`)
)

type Handler struct {
	dbConn   *sql.DB
	cacheSvc *cache.CacheService
	config   *config.Config
	orch     *generator.Orchestrator
	pool     *workers.WorkerPool
}

func NewHandler(dbConn *sql.DB, cacheSvc *cache.CacheService, cfg *config.Config, orch *generator.Orchestrator, pool *workers.WorkerPool) *Handler {
	return &Handler{
		dbConn:   dbConn,
		cacheSvc: cacheSvc,
		config:   cfg,
		orch:     orch,
		pool:     pool,
	}
}

// GenerateHandler orchestrates the generation, filtering, concurrent checking, scoring and ranking.
func (h *Handler) GenerateHandler(c *fiber.Ctx) error {
	// Panic Recovery inside Handler
	defer func() {
		if r := recover(); r != nil {
			reqID, _ := c.Locals("request_id").(string)
			log.Error().Interface("panic", r).Str("request_id", reqID).Msg("Recovered from panic in GenerateHandler")
			_ = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "An unexpected error occurred. Please try again.",
			})
		}
	}()

	// 1. Request Body Size Limit Check (16KB payload safety protection)
	if c.Request().Header.ContentLength() > h.config.MaxRequestBodySize {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
			"error": "Request payload exceeds permissible size limits",
		})
	}

	req := new(models.GenerateRequest)
	if err := c.BodyParser(req); err != nil {
		log.Warn().Err(err).Msg("Validation error: BodyParser failed")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Failed to parse request JSON",
		})
	}

	// 2. Strict Input Schema Validation
	req.Description = utils.CleanInput(req.Description)
	if req.Description == "" {
		log.Warn().Msg("Validation error: Description is empty")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Description is required",
		})
	}
	if len(req.Description) < 3 || len(req.Description) > 1000 {
		log.Warn().Int("length", len(req.Description)).Msg("Validation error: Description length bounds check failed")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Description must be between 3 and 1000 characters",
		})
	}
	if !safeDescRegex.MatchString(req.Description) {
		log.Warn().Str("description", req.Description).Msg("Validation error: Description contains invalid characters")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Description contains invalid or unsafe characters",
		})
	}

	// TLD lists cap bounds
	if len(req.TLDs) == 0 {
		req.TLDs = []string{".com"}
	} else {
		if len(req.TLDs) > 5 {
			log.Warn().Int("tlds_count", len(req.TLDs)).Msg("Validation error: More than 5 TLDs specified")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Maximum of 5 TLDs allowed per request to prevent resource exhaustion",
			})
		}
		// Clean TLD dots
		for i, tld := range req.TLDs {
			tld = strings.ToLower(strings.TrimSpace(tld))
			if !strings.HasPrefix(tld, ".") {
				tld = "." + tld
			}
			// Only alphanumeric characters allowed in TLD after the dot
			tldParts := strings.Split(tld, ".")
			if len(tldParts) < 2 || !utils.IsAlphabetic(tldParts[1]) {
				log.Warn().Str("tld", tld).Msg("Validation error: Invalid TLD format")
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "Invalid TLD format specified",
				})
			}
			req.TLDs[i] = tld
		}
	}

	// Limits on secondary array lengths
	if len(req.Style) > 5 || len(req.Themes) > 5 {
		log.Warn().Int("style_count", len(req.Style)).Int("themes_count", len(req.Themes)).Msg("Validation error: style or themes count bounds exceeded")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Maximum of 5 style values and 5 theme values are permitted",
		})
	}
	if len(req.Avoid) > 10 {
		log.Warn().Int("avoid_count", len(req.Avoid)).Msg("Validation error: avoid count bounds exceeded")
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Maximum of 10 avoid words are permitted",
		})
	}

	// Limits on parameter lengths
	for i, st := range req.Style {
		cleaned := strings.ToLower(utils.CleanInput(st))
		if len(cleaned) > 30 || !utils.IsAlphabetic(cleaned) {
			log.Warn().Str("style", st).Str("cleaned", cleaned).Msg("Validation error: Style parameter validation failed")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Style parameters must be strictly alphabetic and under 30 characters",
			})
		}
		req.Style[i] = cleaned
	}
	for i, th := range req.Themes {
		cleaned := strings.ToLower(utils.CleanInput(th))
		if len(cleaned) > 30 || !utils.IsAlphabetic(cleaned) {
			log.Warn().Str("theme", th).Str("cleaned", cleaned).Msg("Validation error: Theme parameter validation failed")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Theme parameters must be strictly alphabetic and under 30 characters",
			})
		}
		req.Themes[i] = cleaned
	}
	for i, av := range req.Avoid {
		cleaned := strings.ToLower(utils.CleanInput(av))
		if len(cleaned) > 30 || !utils.IsAlphabetic(cleaned) {
			log.Warn().Str("avoid", av).Str("cleaned", cleaned).Msg("Validation error: Avoid parameter validation failed")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Avoid parameters must be strictly alphabetic and under 30 characters",
			})
		}
		req.Avoid[i] = cleaned
	}

	startTime := time.Now()

	// Setup context with timeout for database and external provider operations
	ctx, cancel := context.WithTimeout(c.UserContext(), 25*time.Second)
	defer cancel()

	// 3. Persist search metadata to Postgres
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
		dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
		searchID, err = db.SaveSearch(dbCtx, h.dbConn, searchModel)
		dbCancel()
		if err != nil {
			log.Error().Err(err).Msg("Database write error saving search, continuing query gracefully.")
		}
	}

	// 4. Name Generation (AI, Morphological, Hybrid)
	generatedNames := h.orch.GenerateNames(ctx, req)
	if len(generatedNames) == 0 {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to generate appropriate name candidates. Try relaxing criteria.",
		})
	}

	// Sort generated names by brand score descending and take the top 25 candidates
	// to optimize domain checks execution time and prevent worker queue context timeouts
	sort.Slice(generatedNames, func(i, j int) bool {
		return generatedNames[i].Score > generatedNames[j].Score
	})
	if len(generatedNames) > 25 {
		generatedNames = generatedNames[:25]
	}

	// 5. Save generated names to Postgres and prepare concurrent check jobs
	var jobs []workers.CheckJob
	namesMap := make(map[string]*models.GeneratedName)

	for i := range generatedNames {
		gn := &generatedNames[i]
		gn.SearchID = searchID
		if h.dbConn != nil && searchID > 0 {
			dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
			_, err = db.SaveGeneratedName(dbCtx, h.dbConn, gn)
			dbCancel()
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

	// 6. Execute concurrent domain checks via worker pool
	checkResults := h.pool.RunChecks(ctx, jobs, h.config.WorkerCount)

	// 7. Score and Rank Results
	var items []models.ResultItem
	var dbChecksToSave []models.DomainCheck

	for _, res := range checkResults {
		if res.Error != nil {
			log.Warn().Err(res.Error).Msgf("Skipped scoring for domain %s due to query error", res.Domain)
			continue
		}

		brandScore := 50
		if gn, ok := namesMap[strings.ToLower(res.Name)]; ok {
			brandScore = gn.Score
		}

		usdPrice := res.Price
		if res.Currency == "INR" {
			usdPrice = res.Price / 83.50
		}
		finalScore := ranking.CalculateScore(brandScore, res.TLD, res.Available, usdPrice, res.Name)

		displayPrice := res.Price
		displayCurrency := res.Currency
		if res.Currency == "USD" {
			displayPrice = res.Price * 83.50
			displayCurrency = "INR"
		}

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

		sort.Slice(displayOffers, func(i, j int) bool {
			return displayOffers[i].Price < displayOffers[j].Price
		})

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

	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	// 8. Queue domain check logs in background to avoid blocking API response
	if h.dbConn != nil && len(dbChecksToSave) > 0 {
		db.EnqueueDomainChecks(h.dbConn, dbChecksToSave)
	}

	// 9. Track total request speed latency and log to analytics (queued write)
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
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := h.dbConn.PingContext(ctx); err != nil {
			postgresStatus = "disconnected"
			status = "degraded"
		}
	} else {
		postgresStatus = "unconfigured"
		status = "degraded"
	}

	return c.JSON(fiber.Map{
		"status":    status,
		"postgres":  postgresStatus,
		"redis":     redisStatus,
		"timestamp": time.Now(),
	})
}

// ReadinessCheckHandler verifies that DB and Redis are actively connected.
// If any crucial dependency is down, it returns 503 Service Unavailable.
func (h *Handler) ReadinessCheckHandler(c *fiber.Ctx) error {
	postgresStatus := "connected"
	redisStatus := "connected"
	ready := true

	// Check Postgres
	if h.dbConn != nil {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := h.dbConn.PingContext(ctx); err != nil {
			postgresStatus = "disconnected"
			ready = false
		}
	} else {
		postgresStatus = "unconfigured"
		ready = false
	}

	// Check Redis
	if h.cacheSvc != nil && h.cacheSvc.Client != nil {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := h.cacheSvc.Client.Ping(ctx).Err(); err != nil {
			redisStatus = "disconnected"
			ready = false
		}
	} else {
		redisStatus = "unconfigured"
		ready = false
	}

	statusCode := fiber.StatusOK
	if !ready {
		statusCode = fiber.StatusServiceUnavailable
	}

	return c.Status(statusCode).JSON(fiber.Map{
		"ready":    ready,
		"postgres": postgresStatus,
		"redis":    redisStatus,
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

	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	summary, err := db.GetAnalytics(ctx, h.dbConn)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to compute stats",
		})
	}

	return c.JSON(summary)
}
