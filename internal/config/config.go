package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Config struct {
	AppEnv            string
	Port              string
	LogLevel          zerolog.Level
	PostgresURL       string
	RedisURL          string
	GeminiAPIKey      string
	WorkerCount       int
	CacheTTLHours     int
	RateLimitMax      int
	RateLimitWindowMs int
	PorkbunAPIKey     string
	PorkbunSecretKey  string
	NamecheapUsername string
	NamecheapAPIKey   string
	NamecheapClientIP string
	UseMockProviders  bool
	JWTSecret         string
	AdminAPIKey       string
	MaxRequestBodySize int
	TrustedProxyCount  int
	AllowedOrigins    string
}

// LoadConfig loads application configuration from env variables and/or .env file.
func LoadConfig() *Config {
	// Attempt to load .env file but don't fail if it's missing (e.g. in docker environment)
	if err := godotenv.Load(); err != nil {
		log.Info().Msg("No .env file found, reading configuration directly from system environment variables.")
	}

	appEnv := getEnv("APP_ENV", "development")
	defaultAllowedOrigins := "*"
	if appEnv == "production" || appEnv == "prod" {
		defaultAllowedOrigins = ""
	}

	cfg := &Config{
		AppEnv:            appEnv,
		Port:              getEnv("PORT", "8080"),
		LogLevel:          parseLogLevel(getEnv("LOG_LEVEL", "info")),
		PostgresURL:       getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/nameforge?sslmode=disable"),
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379/0"),
		GeminiAPIKey:      getEnv("GEMINI_API_KEY", getEnv("OPENAI_API_KEY", "")),
		WorkerCount:       getEnvInt("WORKER_COUNT", 20),
		CacheTTLHours:     getEnvInt("CACHE_TTL_HOURS", 24),
		RateLimitMax:      getEnvInt("RATE_LIMIT_MAX", 60),
		RateLimitWindowMs: getEnvInt("RATE_LIMIT_WINDOW_MS", 60000), // 1 minute
		PorkbunAPIKey:     getEnv("PORKBUN_API_KEY", ""),
		PorkbunSecretKey:  getEnv("PORKBUN_SECRET_KEY", ""),
		NamecheapUsername: getEnv("NAMECHEAP_USERNAME", ""),
		NamecheapAPIKey:   getEnv("NAMECHEAP_API_KEY", ""),
		NamecheapClientIP: getEnv("NAMECHEAP_CLIENT_IP", "127.0.0.1"),
		UseMockProviders:  getEnvBool("USE_MOCK_PROVIDERS", true),
		JWTSecret:         getEnv("JWT_SECRET", "nameforge-default-super-secret-key-change-in-prod"),
		AdminAPIKey:       getEnv("ADMIN_API_KEY", "nameforge-admin-secret-seed-key"),
		MaxRequestBodySize: getEnvInt("MAX_REQUEST_BODY_SIZE", 16384), // Default 16KB
		TrustedProxyCount:  getEnvInt("TRUSTED_PROXY_COUNT", 0),
		AllowedOrigins:    getEnv("ALLOWED_ORIGINS", defaultAllowedOrigins),
	}

	return cfg
}

func (c *Config) Validate() error {
	isProd := c.AppEnv == "production" || c.AppEnv == "prod"

	if isProd {
		if c.JWTSecret == "" || c.JWTSecret == "nameforge-default-super-secret-key-change-in-prod" {
			return fmt.Errorf("JWT_SECRET must be set to a secure custom value in production")
		}
		if c.AdminAPIKey == "" || c.AdminAPIKey == "nameforge-admin-secret-seed-key" {
			return fmt.Errorf("ADMIN_API_KEY must be set to a secure custom value in production")
		}
		if c.AllowedOrigins == "" || c.AllowedOrigins == "*" {
			return fmt.Errorf("ALLOWED_ORIGINS cannot be empty or '*' in production for security reasons. Please specify your domain")
		}
		if !c.UseMockProviders && c.GeminiAPIKey == "" {
			return fmt.Errorf("GEMINI_API_KEY must be provided in production when mock providers are disabled")
		}
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}
