package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nameforge/internal/api"
	"nameforge/internal/cache"
	"nameforge/internal/config"
	"nameforge/internal/db"
	"nameforge/internal/generator"
	"nameforge/internal/providers"
	"nameforge/internal/workers"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// 1. Initialize structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	log.Info().Msg("Starting NameForge Engine...")

	// 2. Load Configuration
	cfg := config.LoadConfig()
	zerolog.SetGlobalLevel(cfg.LogLevel)

	// 3. Initialize Postgres
	var dbConn *sql.DB
	var err error
	dbConn, err = db.InitDB(cfg.PostgresURL)
	if err != nil {
		log.Error().Err(err).Msg("Database connection failed. Proceeding with database in read-only/offline mode.")
	} else {
		defer dbConn.Close()
	}

	// 4. Initialize Redis Cache
	cacheSvc, err := cache.InitRedis(cfg.RedisURL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to connect to Redis. Caching will be disabled.")
	}

	// 5. Initialize Domain Providers
	var activeProviders []providers.DomainProvider

	if cfg.UseMockProviders {
		log.Info().Msg("USE_MOCK_PROVIDERS is true. Booting with 5 simulated Mock Providers for comparison.")
		activeProviders = []providers.DomainProvider{
			providers.NewMockProvider("GoDaddy", "INR", 899.0, 449.0, 5299.0, 3299.0),
			providers.NewMockProvider("Hostinger", "INR", 749.0, 399.0, 4999.0, 3199.0),
			providers.NewMockProvider("BigRock", "INR", 929.0, 459.0, 5499.0, 3399.0),
			providers.NewMockProvider("Porkbun", "USD", 9.25, 5.50, 62.50, 36.50),
			providers.NewMockProvider("Namecheap", "USD", 9.88, 5.90, 64.99, 38.99),
		}
	} else {
		log.Info().Msg("Booting with real domain providers. Missing credentials will fallback to mock.")
		
		// 1. GoDaddy
		godaddyKey := os.Getenv("GODADDY_API_KEY")
		godaddySecret := os.Getenv("GODADDY_SECRET_KEY")
		activeProviders = append(activeProviders, providers.NewGoDaddyProvider(godaddyKey, godaddySecret))

		// 2. Hostinger
		hostingerKey := os.Getenv("HOSTINGER_API_KEY")
		activeProviders = append(activeProviders, providers.NewHostingerProvider(hostingerKey))

		// 3. BigRock
		bigrockResellerID := os.Getenv("BIGROCK_RESELLER_ID")
		bigrockKey := os.Getenv("BIGROCK_API_KEY")
		activeProviders = append(activeProviders, providers.NewBigRockProvider(bigrockResellerID, bigrockKey))

		// 4. Porkbun
		porkbunKey := cfg.PorkbunAPIKey
		porkbunSecret := cfg.PorkbunSecretKey
		if porkbunKey != "" && porkbunSecret != "" {
			activeProviders = append(activeProviders, providers.NewPorkbunProvider(porkbunKey, porkbunSecret))
		} else {
			activeProviders = append(activeProviders, providers.NewMockProvider("Porkbun", "USD", 9.25, 5.50, 62.50, 36.50))
		}

		// 5. Namecheap
		namecheapUser := cfg.NamecheapUsername
		namecheapKey := cfg.NamecheapAPIKey
		if namecheapUser != "" && namecheapKey != "" {
			activeProviders = append(activeProviders, providers.NewNamecheapProvider(namecheapUser, namecheapKey, cfg.NamecheapClientIP))
		} else {
			activeProviders = append(activeProviders, providers.NewMockProvider("Namecheap", "USD", 9.88, 5.90, 64.99, 38.99))
		}
	}

	// 6. Instantiate Naming Generator Orchestrator & Worker Pool
	orchestrator := generator.NewOrchestrator(cfg.OpenAIAPIKey)
	cacheTTL := time.Duration(cfg.CacheTTLHours) * time.Hour
	workerPool := workers.NewWorkerPool(activeProviders, cacheSvc, cacheTTL)

	// 7. Initialize Fiber App
	app := fiber.New(fiber.Config{
		AppName:      "NameForge Engine v1.0",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})

	// 8. Bind Routes and Middlewares
	api.SetupRouter(app, dbConn, cacheSvc, cfg, orchestrator, workerPool)

	// 9. Graceful Shutdown Setup
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Info().Msgf("Server closed: %v", err)
		}
	}()

	// Block until signal is received
	sig := <-shutdownChan
	log.Info().Msgf("Signal %v received. Shutting down application gracefully...", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown with errors")
	} else {
		log.Info().Msg("HTTP Server shut down cleanly.")
	}

	log.Info().Msg("NameForge Engine stopped.")
}
