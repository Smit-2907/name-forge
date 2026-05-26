package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"nameforge/internal/models"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type CacheService struct {
	Client *redis.Client
}

// InitRedis connects to Redis and returns a CacheService wrapper.
func InitRedis(url string) (*CacheService, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)

	// Ping to verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Warn().Err(err).Msg("Redis not available, starting with caching disabled")
		return &CacheService{Client: nil}, nil // Allow running without Redis (graceful degradation)
	}

	log.Info().Msg("Connected to Redis successfully.")
	return &CacheService{Client: client}, nil
}

// GetDomainCheck retrieves cached domain check details if present.
func (c *CacheService) GetDomainCheck(ctx context.Context, domain string) (*models.DomainCheck, bool) {
	if c.Client == nil {
		return nil, false
	}

	key := fmt.Sprintf("domain:check:%s", domain)
	val, err := c.Client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, false
	} else if err != nil {
		log.Error().Err(err).Msgf("Redis error getting key %s", key)
		return nil, false
	}

	var check models.DomainCheck
	if err := json.Unmarshal([]byte(val), &check); err != nil {
		log.Error().Err(err).Msgf("Failed to unmarshal cached value for %s", domain)
		return nil, false
	}

	return &check, true
}

// SetDomainCheck caches the domain check details for a specified duration.
func (c *CacheService) SetDomainCheck(ctx context.Context, domain string, check *models.DomainCheck, ttl time.Duration) error {
	if c.Client == nil {
		return nil
	}

	key := fmt.Sprintf("domain:check:%s", domain)
	data, err := json.Marshal(check)
	if err != nil {
		return err
	}

	err = c.Client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		log.Error().Err(err).Msgf("Failed to write to Redis for key %s", key)
	}
	return err
}
