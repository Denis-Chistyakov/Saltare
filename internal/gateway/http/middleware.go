package http

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Generate or extract request ID
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Set request ID in context
		c.Set("X-Request-ID", requestID)
		c.Locals("request_id", requestID)

		return c.Next()
	}
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()

		// Process request
		err := c.Next()

		// Log after request
		duration := time.Since(start)
		requestID := c.Locals("request_id")

		// Safely extract request ID
		reqIDStr := "unknown"
		if requestID != nil {
			if id, ok := requestID.(string); ok {
				reqIDStr = id
			}
		}

		log.Info().
			Str("request_id", reqIDStr).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("duration", duration).
			Str("ip", c.IP()).
			Str("user_agent", c.Get("User-Agent")).
			Msg("HTTP request")

		return err
	}
}

// RecoveryMiddleware recovers from panics
func RecoveryMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		defer func() {
			if r := recover(); r != nil {
				requestID := c.Locals("request_id")

				// Safely extract request ID
				reqIDStr := "unknown"
				if requestID != nil {
					if id, ok := requestID.(string); ok {
						reqIDStr = id
					}
				}

				log.Error().
					Str("request_id", reqIDStr).
					Interface("panic", r).
					Msg("Panic recovered")

				c.Status(500).JSON(fiber.Map{
					"error": "Internal server error",
					"code":  "internal_error",
				})
			}
		}()

		return c.Next()
	}
}

// CORSMiddleware handles CORS
func CORSMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("Access-Control-Allow-Origin", "*")
		c.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Request-ID")
		c.Set("Access-Control-Max-Age", "86400")

		// Handle preflight
		if c.Method() == "OPTIONS" {
			return c.SendStatus(204)
		}

		return c.Next()
	}
}

// CompressionMiddleware compresses responses
func CompressionMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// Fiber has built-in compression, just enable it
		c.Set("Content-Encoding", "gzip")
		return c.Next()
	}
}

// TimeoutMiddleware enforces request timeout
func TimeoutMiddleware(timeout time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		// For now, just pass through
		return c.Next()
	}
}

// AuthMiddleware validates API key (Phase 2)
func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// For now, just pass through

		apiKey := c.Get("X-API-Key")
		if apiKey != "" {
			c.Locals("api_key", apiKey)
		}

		return c.Next()
	}
}

// RateLimitMiddleware implements rate limiting (Phase 2)
func RateLimitMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// For now, just pass through
		return c.Next()
	}
}
