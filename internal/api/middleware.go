package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var (
	// Prometheus metrics
	HttpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nameforge_http_requests_total",
			Help: "Total number of HTTP requests processed.",
		},
		[]string{"path", "status"},
	)
	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nameforge_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)
	DomainChecksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nameforge_domain_checks_total",
			Help: "Total number of domain availability checks executed.",
		},
		[]string{"tld", "available", "cached"},
	)
)

func init() {
	prometheus.MustRegister(HttpRequestsTotal)
	prometheus.MustRegister(HttpRequestDuration)
	prometheus.MustRegister(DomainChecksTotal)
}

// SetupMiddleware registers core global middlewares on Fiber app
func SetupMiddleware(app *fiber.App, rateLimitMax int, rateLimitWindowMs int) {
	// 1. CORS
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	// 2. Structured Logging with Zerolog
	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start)

		statusCode := c.Response().StatusCode()
		log.Info().
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", statusCode).
			Str("ip", c.IP()).
			Dur("duration", duration).
			Msg("HTTP request processed")

		return err
	})

	// 3. Prometheus Metrics Middleware
	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
		// Exclude metrics endpoint from recording its own statistics
		if path == "/metrics" {
			return c.Next()
		}

		start := time.Now()
		err := c.Next()
		duration := time.Since(start).Seconds()

		status := strconv.Itoa(c.Response().StatusCode())
		HttpRequestsTotal.WithLabelValues(path, status).Inc()
		HttpRequestDuration.WithLabelValues(path).Observe(duration)

		return err
	})

	// 4. Rate Limiter (sliding window per IP)
	app.Use(limiter.New(limiter.Config{
		Max:        rateLimitMax,
		Expiration: time.Duration(rateLimitWindowMs) * time.Millisecond,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many requests. Please try again later.",
			})
		},
	}))
}
