package api

import (
	"database/sql"

	"nameforge/internal/cache"
	"nameforge/internal/config"
	"nameforge/internal/generator"
	"nameforge/internal/workers"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// SetupRouter binds paths, setups middleware, and registers handlers.
func SetupRouter(app *fiber.App, dbConn *sql.DB, cacheSvc *cache.CacheService, cfg *config.Config, orch *generator.Orchestrator, pool *workers.WorkerPool) {
	// Initialize core route handler
	h := NewHandler(dbConn, cfg, orch, pool)

	// Middlewares setup (Limiter, Logging, CORS)
	SetupMiddleware(app, cfg.RateLimitMax, cfg.RateLimitWindowMs)

	// REST API Endpoints
	app.Post("/generate", h.GenerateHandler)
	app.Post("/api/generate", h.GenerateHandler)
	app.Get("/health", h.HealthCheckHandler)
	app.Get("/api/analytics", h.AnalyticsHandler)

	// Prometheus Metrics Endpoint adaptation (since standard library handles net/http)
	prometheusHandler := promhttp.Handler()
	app.Get("/metrics", func(c *fiber.Ctx) error {
		fasthttpadaptor.NewFastHTTPHandler(prometheusHandler)(c.Context())
		return nil
	})

	// Serve Static UI Front-end template
	app.Static("/", "./web")
}
