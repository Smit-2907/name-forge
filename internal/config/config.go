package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Config struct {
	Port              string
	LogLevel          zerolog.Level
	PostgresURL       string
	RedisURL          string
	OpenAIAPIKey      string
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
}

// LoadConfig loads application configuration from env variables and/or .env file.
func LoadConfig() *Config {
	// Attempt to load .env file but don't fail if it's missing (e.g. in docker environment)
	if err := godotenv.Load(); err != nil {
		log.Info().Msg("No .env file found, reading configuration directly from system environment variables.")
	}

	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		LogLevel:          parseLogLevel(getEnv("LOG_LEVEL", "info")),
		PostgresURL:       getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/nameforge?sslmode=disable"),
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379/0"),
		OpenAIAPIKey:      getEnv("OPENAI_API_KEY", ""),
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
	}

	return cfg
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
