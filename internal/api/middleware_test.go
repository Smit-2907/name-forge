package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nameforge/internal/config"

	"github.com/gofiber/fiber/v2"
)

func TestMiddleware_SecureHeaders(t *testing.T) {
	app := fiber.New()
	cfg := &config.Config{
		RateLimitMax:      100,
		RateLimitWindowMs: 60000,
		JWTSecret:         "test",
		AdminAPIKey:       "admin",
	}

	SetupMiddleware(app, nil, nil, cfg)

	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Failed to test request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %d", resp.StatusCode)
	}

	// Verify security headers exist
	headers := []string{
		"Content-Security-Policy",
		"X-Frame-Options",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"X-XSS-Protection",
		"Strict-Transport-Security",
	}

	for _, h := range headers {
		if val := resp.Header.Get(h); val == "" {
			t.Errorf("Expected header %s to be set", h)
		}
	}

	// Request ID check
	if reqID := resp.Header.Get("X-Request-ID"); reqID == "" {
		t.Error("Expected X-Request-ID header to be set")
	}
}

func TestMiddleware_AdminAuthentication(t *testing.T) {
	app := fiber.New()
	cfg := &config.Config{
		RateLimitMax:      10,
		RateLimitWindowMs: 60000,
		AdminAPIKey:       "super-secret-admin-key",
	}

	SetupMiddleware(app, nil, nil, cfg)

	app.Get("/test", func(c *fiber.Ctx) error {
		user := c.Locals("user_id").(string)
		auth := c.Locals("authenticated").(bool)
		return c.JSON(fiber.Map{"user": user, "auth": auth})
	})

	// Test authorized request with admin key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer super-secret-admin-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("App test failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected OK status with valid admin key, got %d", resp.StatusCode)
	}

	// Test public unauthenticated request
	reqPub := httptest.NewRequest("GET", "/test", nil)
	respPub, err := app.Test(reqPub)
	if err != nil {
		t.Fatalf("App test failed: %v", err)
	}
	if respPub.StatusCode != http.StatusOK {
		t.Errorf("Expected OK status for public request, got %d", respPub.StatusCode)
	}
}
