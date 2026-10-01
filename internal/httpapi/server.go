package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/simpul/hr-backend/internal/config"
	"github.com/simpul/hr-backend/internal/platform/secure"
	"github.com/simpul/hr-backend/internal/repository"
	"github.com/simpul/hr-backend/openapi"
	"gorm.io/gorm"
)

type Server struct {
	config     config.Config
	db         *gorm.DB
	store      *repository.Store
	redis      *redis.Client
	tokens     *secure.TokenManager
	cipher     *secure.Cipher
	logger     *slog.Logger
	hub        *Hub
	rateMu     sync.Mutex
	loginRates map[string][]time.Time
}

func NewServer(cfg config.Config, db *gorm.DB, redisClient *redis.Client, logger *slog.Logger) (*Server, error) {
	cipher, err := secure.NewCipher(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	server := &Server{
		config: cfg, db: db, store: repository.New(db), redis: redisClient,
		tokens: secure.NewTokenManager(cfg.AccessTokenSecret, cfg.AccessTokenTTL), cipher: cipher,
		logger: logger, loginRates: map[string][]time.Time{},
	}
	server.hub = NewHub(redisClient, logger, cfg.AllowedOrigins)
	return server, nil
}

func (s *Server) Router() *gin.Engine {
	if s.config.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	// Without this a wrong method on a known path falls through to the 404 handler, so a
	// client cannot tell "no such endpoint" from "wrong verb" — the signal that reveals
	// client/server version skew.
	router.HandleMethodNotAllowed = true
	router.Use(gin.Recovery(), requestMiddleware(), s.accessLog(), s.cors())
	router.GET("/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/health/ready", s.readiness)
	router.GET("/openapi.yaml", s.openapiDocument)
	router.NoRoute(s.notFound)
	router.NoMethod(s.methodNotAllowed)

	v1 := router.Group("/v1")
	{
		auth := v1.Group("/auth")
		auth.POST("/login", s.loginRateLimit(), s.login)
		auth.POST("/refresh", s.refresh)
		auth.POST("/logout", s.logout)
		auth.POST("/mfa/enroll", s.mfaEnroll)
		auth.POST("/mfa/confirm", s.mfaConfirm)
		auth.POST("/mfa/verify", s.mfaVerify)
		auth.GET("/sso/:provider/start", s.ssoStart)
		auth.GET("/sso/:provider/callback", s.ssoCallback)
		v1.GET("/ws", s.websocket)

		protected := v1.Group("")
		protected.Use(s.authRequired())
		s.registerCoreRoutes(protected)
		s.registerOperationsRoutes(protected)
		s.registerTalentRoutes(protected)
		s.registerCollaborationRoutes(protected)
	}
	return router
}

// openapiDocument serves the contract out of the binary instead of from disk. Reading
// "./openapi/openapi.yaml" resolved against the process working directory, so this endpoint
// answered 200 under `go run` from the backend directory and a plain-text 404 from anywhere
// else — a container, systemd, or a compiled binary started elsewhere — while every other
// route stayed healthy, which made it look like a missing endpoint rather than a missing
// file. See openapi/embed.go.
func (s *Server) openapiDocument(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", openapi.Document)
}

// notFound and methodNotAllowed keep unmatched requests inside the error contract.
//
// Gin's defaults answer with a plain-text "404 page not found", but the specification
// declares a Problem document as the default response for every operation, so a generated
// client parses that body as JSON and fails on it. Every other error this API emits is a
// Problem; these two were the only exceptions.
func (s *Server) notFound(c *gin.Context) {
	problem(c, http.StatusNotFound, "not_found", "Endpoint tidak ditemukan",
		"Rute yang diminta tidak terdaftar pada API ini.", nil)
}

func (s *Server) methodNotAllowed(c *gin.Context) {
	// Gin populates Allow before invoking this handler when HandleMethodNotAllowed is on, so
	// the response tells the client which verbs the path does accept.
	problem(c, http.StatusMethodNotAllowed, "method_not_allowed", "Metode tidak didukung",
		"Metode HTTP ini tidak tersedia untuk rute tersebut.", nil)
}

func (s *Server) cors() gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, origin := range s.config.AllowedOrigins {
		allowed[origin] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		s.logger.Info("http request", "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "durationMs", time.Since(start).Milliseconds(), "requestId", c.GetString("requestId"))
	}
}

func (s *Server) loginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if s.redis != nil {
			redisKey := "simpul:ratelimit:login:" + key
			ctx := c.Request.Context()
			count, err := s.redis.Incr(ctx, redisKey).Result()
			if err == nil {
				if count == 1 {
					s.redis.Expire(ctx, redisKey, time.Minute)
				}
				if count > 10 {
					problem(c, http.StatusTooManyRequests, "rate_limited", "Terlalu banyak percobaan", "Coba lagi dalam satu menit.", nil)
					c.Abort()
					return
				}
				c.Next()
				return
			}
		}

		now := time.Now()
		s.rateMu.Lock()
		kept := s.loginRates[key][:0]
		for _, attempt := range s.loginRates[key] {
			if now.Sub(attempt) < time.Minute {
				kept = append(kept, attempt)
			}
		}
		if len(kept) >= 10 {
			s.loginRates[key] = kept
			s.rateMu.Unlock()
			problem(c, http.StatusTooManyRequests, "rate_limited", "Terlalu banyak percobaan", "Coba lagi dalam satu menit.", nil)
			c.Abort()
			return
		}
		s.loginRates[key] = append(kept, now)
		s.rateMu.Unlock()
		c.Next()
	}
}

// redisAvailable reports whether Redis can serve requests right now. The client is
// non-nil whenever REDIS_ADDR is configured, even if no server is listening, so a
// nil check alone is not enough to decide that Redis is usable.
// A readiness probe has to answer quickly and predictably, because an orchestrator kills
// or unroutes a pod whose probe exceeds its timeout — Kubernetes defaults to one second.
// The probe inherits the latency of whatever it pings, and a Redis that is merely absent
// is not the worst case: go-redis retries, so an unreachable or hanging Redis holds this
// handler for seconds. That would report a perfectly healthy application as unready.
//
// Redis is optional by design, so a missing Redis must not make the service unready — it
// is reported as degraded. These budgets only bound how long we are willing to wait to
// find that out.
//
// The Redis budget is deliberately much shorter than the overall one: a ping is answered in
// microseconds by a healthy server, so 300ms is already generous, and keeping it small is
// what holds the whole response comfortably inside a one-second probe.
const (
	readinessTimeout  = 2 * time.Second
	redisProbeTimeout = 300 * time.Millisecond
)

func (s *Server) redisAvailable(ctx context.Context) bool {
	return s.redis != nil && s.redis.Ping(ctx).Err() == nil
}

// redisStatus distinguishes three states, because collapsing them costs an operator real time:
//
//	ok       — configured and answering
//	degraded — configured but not answering; the app still serves, with realtime and the
//	           queue unavailable
//	disabled — not configured at all; there is nothing to fix
//
// Reporting "degraded" when REDIS_ADDR was never set sends someone looking for a Redis
// problem that does not exist.
func (s *Server) redisStatus(ctx context.Context) string {
	if s.redis == nil {
		return "disabled"
	}
	// Its own, shorter budget: the common case is a Redis that is simply not running, and
	// there is no reason for that to consume the whole readiness window.
	probeCtx, cancel := context.WithTimeout(ctx, redisProbeTimeout)
	defer cancel()
	if s.redisAvailable(probeCtx) {
		return "ok"
	}
	return "degraded"
}

func (s *Server) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
	defer cancel()

	sqlDB, err := s.db.DB()
	if err != nil || sqlDB.PingContext(ctx) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "postgres": "unavailable", "redis": "unknown"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready", "postgres": "ok", "redis": s.redisStatus(ctx)})
}

func providerConfigured(cfg config.Config, provider string) bool {
	switch strings.ToLower(provider) {
	case "google":
		return cfg.GoogleClientID != "" && cfg.GoogleClientSecret != ""
	case "entra":
		return cfg.EntraClientID != "" && cfg.EntraClientSecret != ""
	default:
		return false
	}
}

func (s *Server) serveCachedJSON(c *gin.Context, key string) bool {
	if s.redis == nil {
		return false
	}
	raw, err := s.redis.Get(c.Request.Context(), key).Bytes()
	if err != nil {
		return false
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", raw)
	return true
}

func (s *Server) writeCachedJSON(c *gin.Context, key string, value any, ttl time.Duration) {
	if s.redis == nil {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	if err := s.redis.Set(c.Request.Context(), key, raw, ttl).Err(); err != nil {
		s.logger.Debug("cache write skipped", "key", key, "error", err)
	}
}
