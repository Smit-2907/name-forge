package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"database/sql"
	"nameforge/internal/cache"
	"nameforge/internal/config"
	"nameforge/internal/db"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
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
	RateLimitedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nameforge_rate_limited_requests_total",
			Help: "Total number of requests rejected due to rate limiting.",
		},
		[]string{"path", "auth_status"},
	)
)

func init() {
	prometheus.MustRegister(HttpRequestsTotal)
	prometheus.MustRegister(HttpRequestDuration)
	prometheus.MustRegister(DomainChecksTotal)
	prometheus.MustRegister(RateLimitedRequests)
}

// LocalLimiter handles sliding window in-memory fallback rate limiting.
type LocalLimiter struct {
	mu     sync.Mutex
	tracks map[string][]time.Time
}

func NewLocalLimiter() *LocalLimiter {
	return &LocalLimiter{
		tracks: make(map[string][]time.Time),
	}
}

func (l *LocalLimiter) Allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	times := l.tracks[key]
	var active []time.Time

	for _, t := range times {
		if t.After(cutoff) {
			active = append(active, t)
		}
	}

	if len(active) >= limit {
		l.tracks[key] = active
		return false
	}

	active = append(active, now)
	l.tracks[key] = active
	return true
}

var localRateLimiter = NewLocalLimiter()

// GetClientIP retrieves the real client IP safely by respecting trusted proxy boundaries.
func GetClientIP(c *fiber.Ctx, trustedProxyCount int) string {
	if trustedProxyCount > 0 {
		xForwardedFor := c.Get("X-Forwarded-For")
		if xForwardedFor != "" {
			parts := strings.Split(xForwardedFor, ",")
			if len(parts) >= trustedProxyCount {
				targetIP := strings.TrimSpace(parts[len(parts)-trustedProxyCount])
				if targetIP != "" {
					return targetIP
				}
			}
		}
	}
	// Fallback proxy-specific headers
	if ip := c.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if ip := c.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return c.IP()
}

// SetupMiddleware registers secure HTTP headers, correlation logging, authentication, and rate-limiting.
func SetupMiddleware(app *fiber.App, dbConn *sql.DB, cacheSvc *cache.CacheService, cfg *config.Config) {
	// 1. Secure HTTP Headers & CSP
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdnjs.cloudflare.com; font-src 'self' https://fonts.gstatic.com https://cdnjs.cloudflare.com; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self';")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		return c.Next()
	})

	// 2. CORS Hardening
	app.Use(cors.New(cors.Config{
		AllowOrigins: cfg.AllowedOrigins,
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-API-Key",
		AllowMethods: "GET, POST, OPTIONS",
	}))

	// 3. Correlation Request ID Middleware
	app.Use(func(c *fiber.Ctx) error {
		reqID := c.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		c.Set("X-Request-ID", reqID)
		c.Locals("request_id", reqID)
		return c.Next()
	})

	// 4. Structured Logging with Zerolog & Correlation IDs
	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		duration := time.Since(start)

		statusCode := c.Response().StatusCode()
		reqID, _ := c.Locals("request_id").(string)
		userID, _ := c.Locals("user_id").(string)

		log.Info().
			Str("request_id", reqID).
			Str("user_id", userID).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", statusCode).
			Str("ip", GetClientIP(c, cfg.TrustedProxyCount)).
			Dur("duration", duration).
			Msg("HTTP request processed")

		return err
	})

	// 5. Prometheus Metrics Middleware
	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
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

	// 6. Authentication Middleware
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("authenticated", false)
		c.Locals("rate_limit_max", cfg.RateLimitMax) // Fallback standard limit
		c.Locals("user_id", "public")

		var tokenStr string
		authHeader := c.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		} else if apiKeyHeader := c.Get("X-API-Key"); apiKeyHeader != "" {
			tokenStr = apiKeyHeader
		}

		if tokenStr == "" {
			return c.Next()
		}

		// Check Admin API key
		if tokenStr == cfg.AdminAPIKey {
			c.Locals("authenticated", true)
			c.Locals("rate_limit_max", cfg.RateLimitMax*10)
			c.Locals("user_id", "admin")
			return c.Next()
		}

		// Check JWT Token (indicated by dot format)
		if strings.Count(tokenStr, ".") == 2 {
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing algorithm: %v", t.Header["alg"])
				}
				return []byte(cfg.JWTSecret), nil
			})

			if err == nil && token.Valid {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					sub, _ := claims["sub"].(string)
					limitMax := cfg.RateLimitMax
					if customLimit, ok := claims["rate_limit"].(float64); ok {
						limitMax = int(customLimit)
					}
					c.Locals("authenticated", true)
					c.Locals("rate_limit_max", limitMax)
					c.Locals("user_id", "jwt:"+sub)
					return c.Next()
				}
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid or expired authorization token",
			})
		}

		// Otherwise, treat as Postgres database API Key
		if dbConn != nil {
			userID, limitMax, active, err := db.VerifyAPIKey(c.Context(), dbConn, tokenStr)
			if err == nil && active {
				c.Locals("authenticated", true)
				c.Locals("rate_limit_max", limitMax)
				c.Locals("user_id", "apikey:"+userID)
				return c.Next()
			}
		}

		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid or inactive API key",
		})
	})

	// 7. Resilient Sliding Window Rate Limiter Middleware
	app.Use(func(c *fiber.Ctx) error {
		path := c.Path()
		if path == "/metrics" || path == "/health" {
			return c.Next()
		}

		userID, _ := c.Locals("user_id").(string)
		limitMax, _ := c.Locals("rate_limit_max").(int)
		authStatus := "unauthenticated"
		if authVal, ok := c.Locals("authenticated").(bool); ok && authVal {
			authStatus = "authenticated"
		}

		clientIP := GetClientIP(c, cfg.TrustedProxyCount)
		rateLimitKey := fmt.Sprintf("ratelimit:ip:%s", clientIP)
		if userID != "public" {
			rateLimitKey = fmt.Sprintf("ratelimit:user:%s", userID)
		}

		window := time.Duration(cfg.RateLimitWindowMs) * time.Millisecond
		allowed := false

		// Redis Sliding Window Check
		if cacheSvc != nil && cacheSvc.Client != nil {
			var err error
			rCtx, rCancel := context.WithTimeout(c.Context(), 250*time.Millisecond)
			allowed, err = checkRedisRateLimit(rCtx, cacheSvc.Client, rateLimitKey, limitMax, window)
			rCancel()
			if err != nil {
				log.Warn().Err(err).Msgf("Redis rate limiter failed, falling back to local memory limit for %s", rateLimitKey)
				// Fallback to local memory limiter
				allowed = localRateLimiter.Allow(rateLimitKey, limitMax, window)
			}
		} else {
			// Fallback local memory limiter
			allowed = localRateLimiter.Allow(rateLimitKey, limitMax, window)
		}

		if !allowed {
			RateLimitedRequests.WithLabelValues(path, authStatus).Inc()
			c.Set("Retry-After", strconv.Itoa(cfg.RateLimitWindowMs/1000))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Rate limit exceeded. Please check quotas or try again later.",
			})
		}

		return c.Next()
	})
}

// checkRedisRateLimit evaluates rate limit using an atomic Lua script in Redis.
func checkRedisRateLimit(ctx context.Context, client *redis.Client, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	clearBefore := now - windowMs

	script := `
		redis.call('ZREMRANGEBYSCORE', KEYS[1], 0, ARGV[2])
		local count = redis.call('ZCARD', KEYS[1])
		if count < tonumber(ARGV[3]) then
			redis.call('ZADD', KEYS[1], ARGV[1], ARGV[1])
			redis.call('EXPIRE', KEYS[1], tonumber(ARGV[4]))
			return 1
		else
			return 0
		end
	`

	res, err := client.Eval(ctx, script, []string{key}, now, clearBefore, limit, int(window.Seconds())).Result()
	if err != nil {
		return false, err
	}

	allowed, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected script response type: %T", res)
	}

	return allowed == 1, nil
}
