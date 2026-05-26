package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"nameforge/internal/models"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

type dbLogTask func(db *sql.DB)

var (
	logQueue     = make(chan dbLogTask, 10000) // bounded queue
	logQueueWg   sync.WaitGroup
	logQueueOnce sync.Once
)

// StartBackgroundLogger spawns workers to process database writes.
func StartBackgroundLogger(db *sql.DB, numWorkers int) {
	logQueueOnce.Do(func() {
		for i := 0; i < numWorkers; i++ {
			logQueueWg.Add(1)
			go func() {
				defer logQueueWg.Done()
				// Recovery in case a background write panics
				defer func() {
					if r := recover(); r != nil {
						log.Error().Msgf("Recovered from background logger panic: %v", r)
					}
				}()

				for task := range logQueue {
					task(db)
				}
			}()
		}
		log.Info().Msgf("Started %d background database logging workers.", numWorkers)
	})
}

// StopBackgroundLogger gracefully shuts down the logger and flushes the queue.
func StopBackgroundLogger() {
	close(logQueue)
	// Wait with timeout
	done := make(chan struct{})
	go func() {
		logQueueWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info().Msg("Background database logger shut down cleanly.")
	case <-time.After(5 * time.Second):
		log.Warn().Msg("Background database logger shutdown timed out. Some logs may have been dropped.")
	}
}

// EnqueueDBTask puts a database task on the bounded queue.
func EnqueueDBTask(task dbLogTask) bool {
	select {
	case logQueue <- task:
		return true
	default:
		log.Warn().Msg("Database log queue is full, dropping write task.")
		return false
	}
}

// InitDB configures the database connection pool and creates schema if missing.
func InitDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify database connection
	var errPing error
	for i := 0; i < 5; i++ {
		errPing = db.Ping()
		if errPing == nil {
			break
		}
		log.Warn().Err(errPing).Msgf("PostgreSQL not ready yet, retrying... (%d/5)", i+1)
		time.Sleep(2 * time.Second)
	}
	if errPing != nil {
		return nil, errPing
	}

	log.Info().Msg("Connected to PostgreSQL successfully. Running auto-migrations...")
	if err := migrateSchema(db); err != nil {
		return nil, err
	}

	// Start background log workers
	StartBackgroundLogger(db, 3)

	return db, nil
}

func migrateSchema(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS searches (
			id BIGSERIAL PRIMARY KEY,
			description TEXT NOT NULL,
			style TEXT[] NOT NULL,
			themes TEXT[] NOT NULL,
			tlds TEXT[] NOT NULL,
			avoid TEXT[] NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS generated_names (
			id BIGSERIAL PRIMARY KEY,
			search_id BIGINT REFERENCES searches(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			generator_type VARCHAR(50) NOT NULL,
			score INT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS domain_checks (
			id BIGSERIAL PRIMARY KEY,
			name_id BIGINT REFERENCES generated_names(id) ON DELETE CASCADE,
			domain VARCHAR(255) NOT NULL,
			tld VARCHAR(50) NOT NULL,
			available BOOLEAN NOT NULL,
			price DECIMAL(10,2) NOT NULL,
			currency VARCHAR(10) NOT NULL,
			platform VARCHAR(100) NOT NULL DEFAULT 'Unknown',
			offers TEXT,
			checked_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`ALTER TABLE domain_checks ADD COLUMN IF NOT EXISTS platform VARCHAR(100) NOT NULL DEFAULT 'Unknown';`,
		`ALTER TABLE domain_checks ADD COLUMN IF NOT EXISTS offers TEXT;`,
		`CREATE TABLE IF NOT EXISTS analytics_events (
			id BIGSERIAL PRIMARY KEY,
			event_type VARCHAR(100) NOT NULL,
			metric_value DOUBLE PRECISION DEFAULT 0.0,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id BIGSERIAL PRIMARY KEY,
			key_hash VARCHAR(64) UNIQUE NOT NULL,
			user_id VARCHAR(100) NOT NULL,
			rate_limit_max INT NOT NULL DEFAULT 100,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		);`,
		// Seed a default developer key for testing if table is empty
		// SHA256 of "nf_dev_key_2026" is "7bc9103c80cf8c4a96bde0ef7072ce9f1a260840b2d69e8bd802081d5967ee38"
		`INSERT INTO api_keys (key_hash, user_id, rate_limit_max, is_active)
		 VALUES ('7bc9103c80cf8c4a96bde0ef7072ce9f1a260840b2d69e8bd802081d5967ee38', 'dev_user', 100, true)
		 ON CONFLICT (key_hash) DO NOTHING;`,
	}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			log.Error().Err(err).Msgf("Failed executing migration statement: %s", q)
			return err
		}
	}

	log.Info().Msg("PostgreSQL migrations completed successfully.")
	return nil
}

// VerifyAPIKey hashes the raw API Key and checks if it exists and is active in database.
func VerifyAPIKey(ctx context.Context, db *sql.DB, key string) (string, int, bool, error) {
	hashBytes := sha256.Sum256([]byte(key))
	hashHex := hex.EncodeToString(hashBytes[:])

	query := `SELECT user_id, rate_limit_max, is_active FROM api_keys WHERE key_hash = $1`
	var userID string
	var limitMax int
	var active bool

	err := db.QueryRowContext(ctx, query, hashHex).Scan(&userID, &limitMax, &active)
	if err != nil {
		return "", 0, false, err
	}
	return userID, limitMax, active, nil
}

// SaveSearch stores a new search request metadata.
func SaveSearch(ctx context.Context, db *sql.DB, s *models.Search) (int64, error) {
	if s.Style == nil {
		s.Style = []string{}
	}
	if s.Themes == nil {
		s.Themes = []string{}
	}
	if s.TLDs == nil {
		s.TLDs = []string{}
	}
	if s.Avoid == nil {
		s.Avoid = []string{}
	}

	query := `INSERT INTO searches (description, style, themes, tlds, avoid, created_at)
			  VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	var id int64
	err := db.QueryRowContext(ctx, query, s.Description, pq.Array(s.Style), pq.Array(s.Themes), pq.Array(s.TLDs), pq.Array(s.Avoid), time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save search to PostgreSQL")
		return 0, err
	}
	s.ID = id
	return id, nil
}

// SaveGeneratedName stores a generated name linked to a search.
func SaveGeneratedName(ctx context.Context, db *sql.DB, gn *models.GeneratedName) (int64, error) {
	query := `INSERT INTO generated_names (search_id, name, generator_type, score, created_at)
			  VALUES ($1, $2, $3, $4, $5) RETURNING id`
	var id int64
	err := db.QueryRowContext(ctx, query, gn.SearchID, gn.Name, gn.GeneratorType, gn.Score, time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save generated name to PostgreSQL")
		return 0, err
	}
	gn.ID = id
	return id, nil
}

// SaveDomainCheck logs domain availability and pricing results.
func SaveDomainCheck(ctx context.Context, db *sql.DB, dc *models.DomainCheck) (int64, error) {
	var offersJSON []byte
	var err error
	if len(dc.Offers) > 0 {
		offersJSON, err = json.Marshal(dc.Offers)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal offers for database write")
		}
	}
	offersStr := string(offersJSON)

	query := `INSERT INTO domain_checks (name_id, domain, tld, available, price, currency, platform, offers, checked_at)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`
	var id int64
	err = db.QueryRowContext(ctx, query, dc.NameID, dc.Domain, dc.TLD, dc.Available, dc.Price, dc.Currency, dc.Platform, offersStr, time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save domain check to PostgreSQL")
		return 0, err
	}
	dc.ID = id
	return id, nil
}

// EnqueueDomainChecks adds domain check writes to the background log queue.
func EnqueueDomainChecks(db *sql.DB, checks []models.DomainCheck) {
	for _, check := range checks {
		c := check // capture variable
		EnqueueDBTask(func(d *sql.DB) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if c.NameID > 0 {
				_, _ = SaveDomainCheck(ctx, d, &c)
			}
		})
	}
}

// LogAnalyticsEvent saves custom metric ticks (e.g. search speed latency) to the queue.
func LogAnalyticsEvent(db *sql.DB, eventType string, value float64) {
	EnqueueDBTask(func(d *sql.DB) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		query := `INSERT INTO analytics_events (event_type, metric_value) VALUES ($1, $2)`
		if _, err := d.ExecContext(ctx, query, eventType, value); err != nil {
			log.Error().Err(err).Msg("Failed to log analytics event to PostgreSQL")
		}
	})
}

// GetAnalytics computes stats from tables.
func GetAnalytics(ctx context.Context, db *sql.DB) (*models.AnalyticsSummary, error) {
	summary := &models.AnalyticsSummary{}

	// Total searches
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM searches").Scan(&summary.TotalSearches)
	if err != nil {
		return nil, err
	}

	// Total names generated
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM generated_names").Scan(&summary.TotalNamesGen)
	if err != nil {
		return nil, err
	}

	// Total domain checks
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domain_checks").Scan(&summary.TotalDomainChecks)
	if err != nil {
		return nil, err
	}

	// Availability rate
	var availableCount int64
	if summary.TotalDomainChecks > 0 {
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domain_checks WHERE available = true").Scan(&availableCount)
		if err == nil {
			summary.AvailabilityRate = float64(availableCount) / float64(summary.TotalDomainChecks) * 100
		}
	}

	// Average search speed (using logged analytics event type 'search_latency_ms')
	err = db.QueryRowContext(ctx, "SELECT COALESCE(AVG(metric_value), 0.0) FROM analytics_events WHERE event_type = 'search_latency_ms'").Scan(&summary.AverageSearchSpeed)
	if err != nil {
		summary.AverageSearchSpeed = 0.0
	}

	return summary, nil
}
