package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Hub struct {
	redis          *redis.Client
	logger         *slog.Logger
	allowedOrigins map[string]bool
}

func NewHub(redisClient *redis.Client, logger *slog.Logger, origins []string) *Hub {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[origin] = true
	}
	return &Hub{redis: redisClient, logger: logger, allowedOrigins: allowed}
}

func (h *Hub) Publish(ctx context.Context, organizationID string, event any) {
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	if h.redis != nil {
		if err := h.redis.Publish(ctx, "simpul:org:"+organizationID, raw).Err(); err != nil {
			h.logger.Warn("redis realtime publish failed", "error", err)
		}
	}
}

func (h *Hub) Serve(c *gin.Context, organizationID string) {
	if h.redis == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || h.allowedOrigins[origin]
	}, ReadBufferSize: 1024, WriteBufferSize: 1024}
	connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	subscription := h.redis.Subscribe(c.Request.Context(), "simpul:org:"+organizationID)
	defer subscription.Close()
	if _, err := subscription.Receive(c.Request.Context()); err != nil {
		h.logger.Warn("redis realtime subscribe failed", "error", err)
		return
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case message, ok := <-subscription.Channel():
				if !ok || connection.WriteMessage(websocket.TextMessage, []byte(message.Payload)) != nil {
					return
				}
			case <-ticker.C:
				if connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	connection.SetReadLimit(4096)
	_ = connection.SetReadDeadline(time.Now().Add(70 * time.Second))
	connection.SetPongHandler(func(string) error { return connection.SetReadDeadline(time.Now().Add(70 * time.Second)) })
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
	}
}
