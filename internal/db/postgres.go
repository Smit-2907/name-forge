package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"nameforge/internal/models"

	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

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

// SaveSearch stores a new search request metadata.
func SaveSearch(db *sql.DB, s *models.Search) (int64, error) {
	query := `INSERT INTO searches (description, style, themes, tlds, avoid, created_at)
			  VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`
	var id int64
	err := db.QueryRow(query, s.Description, pq.Array(s.Style), pq.Array(s.Themes), pq.Array(s.TLDs), pq.Array(s.Avoid), time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save search to PostgreSQL")
		return 0, err
	}
	s.ID = id
	return id, nil
}

// SaveGeneratedName stores a generated name linked to a search.
func SaveGeneratedName(db *sql.DB, gn *models.GeneratedName) (int64, error) {
	query := `INSERT INTO generated_names (search_id, name, generator_type, score, created_at)
			  VALUES ($1, $2, $3, $4, $5) RETURNING id`
	var id int64
	err := db.QueryRow(query, gn.SearchID, gn.Name, gn.GeneratorType, gn.Score, time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save generated name to PostgreSQL")
		return 0, err
	}
	gn.ID = id
	return id, nil
}

// SaveDomainCheck logs domain availability and pricing results.
func SaveDomainCheck(db *sql.DB, dc *models.DomainCheck) (int64, error) {
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
	err = db.QueryRow(query, dc.NameID, dc.Domain, dc.TLD, dc.Available, dc.Price, dc.Currency, dc.Platform, offersStr, time.Now()).Scan(&id)
	if err != nil {
		log.Error().Err(err).Msg("Failed to save domain check to PostgreSQL")
		return 0, err
	}
	dc.ID = id
	return id, nil
}

// LogAnalyticsEvent saves custom metric ticks (e.g. search speed latency).
func LogAnalyticsEvent(db *sql.DB, eventType string, value float64) {
	go func() {
		query := `INSERT INTO analytics_events (event_type, metric_value) VALUES ($1, $2)`
		if _, err := db.Exec(query, eventType, value); err != nil {
			log.Error().Err(err).Msg("Failed to log analytics event to PostgreSQL")
		}
	}()
}

// GetAnalytics computes stats from tables.
func GetAnalytics(db *sql.DB) (*models.AnalyticsSummary, error) {
	summary := &models.AnalyticsSummary{}

	// Total searches
	err := db.QueryRow("SELECT COUNT(*) FROM searches").Scan(&summary.TotalSearches)
	if err != nil {
		return nil, err
	}

	// Total names generated
	err = db.QueryRow("SELECT COUNT(*) FROM generated_names").Scan(&summary.TotalNamesGen)
	if err != nil {
		return nil, err
	}

	// Total domain checks
	err = db.QueryRow("SELECT COUNT(*) FROM domain_checks").Scan(&summary.TotalDomainChecks)
	if err != nil {
		return nil, err
	}

	// Availability rate
	var availableCount int64
	if summary.TotalDomainChecks > 0 {
		err = db.QueryRow("SELECT COUNT(*) FROM domain_checks WHERE available = true").Scan(&availableCount)
		if err == nil {
			summary.AvailabilityRate = float64(availableCount) / float64(summary.TotalDomainChecks) * 100
		}
	}

	// Average search speed (using logged analytics event type 'search_latency_ms')
	err = db.QueryRow("SELECT COALESCE(AVG(metric_value), 0.0) FROM analytics_events WHERE event_type = 'search_latency_ms'").Scan(&summary.AverageSearchSpeed)
	if err != nil {
		// Fallback/log error but do not fail
		summary.AverageSearchSpeed = 0.0
	}

	return summary, nil
}
