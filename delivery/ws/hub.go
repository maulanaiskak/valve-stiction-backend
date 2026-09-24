// Package ws is the WebSocket delivery adapter: tracks connected clients
// and polls usecase.SensorService for changes to push. No DB access here
// -- that's usecase/repository's job.
package ws

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/maulanaiskak/valve-stiction-backend/domain"
	"github.com/maulanaiskak/valve-stiction-backend/usecase"
)

// PollInterval: how often the hub checks for new results. No pub/sub
// between ingestion and backend (two independent processes, no existing
// shared channel) -- a short poll is simple, correct, and cheap at this
// data volume. See docs/V3_PLAN.md.
const PollInterval = 1 * time.Second

var upgrader = websocket.Upgrader{
	// Local/portfolio deployment only, no cross-origin browser clients to
	// guard against.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Hub tracks connected WebSocket clients and the last status it broadcast
// per sensor, so it only sends when something actually changed.
type Hub struct {
	svc *usecase.SensorService

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
	last    map[string]domain.SensorStatus // sensor_id -> last broadcast status
}

func NewHub(svc *usecase.SensorService) *Hub {
	return &Hub{
		svc:     svc,
		clients: make(map[*websocket.Conn]struct{}),
		last:    make(map[string]domain.SensorStatus),
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = struct{}{}
	// send whatever we currently know immediately, so a new client isn't
	// stuck showing nothing until the next changed status.
	initial := make([]domain.SensorStatus, 0, len(h.last))
	for _, s := range h.last {
		initial = append(initial, s)
	}
	h.mu.Unlock()

	if len(initial) > 0 {
		if payload, err := json.Marshal(map[string]any{"type": "snapshot", "sensors": initial}); err == nil {
			_ = conn.WriteMessage(websocket.TextMessage, payload)
		}
	}

	// Drain and discard any client->server messages (none expected) so the
	// connection's read side stays serviced and close/errors are detected.
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (h *Hub) broadcast(update domain.SensorStatus) {
	payload, err := json.Marshal(map[string]any{"type": "update", "sensor": update})
	if err != nil {
		log.Printf("failed to marshal update: %v", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			log.Printf("write failed, dropping client: %v", err)
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

// Run polls on PollInterval and broadcasts any sensor whose latest
// window_start moved forward since the last poll -- diff-based so idle
// sensors don't spam connected clients with identical state.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			statuses, err := h.svc.LatestStatuses(ctx)
			if err != nil {
				log.Printf("poll failed: %v", err)
				continue
			}

			h.mu.Lock()
			for _, s := range statuses {
				prev, ok := h.last[s.SensorID]
				if ok && !s.WindowStart.After(prev.WindowStart) {
					continue // no new window for this sensor
				}
				h.last[s.SensorID] = s
				h.mu.Unlock()
				h.broadcast(s)
				h.mu.Lock()
			}
			h.mu.Unlock()
		}
	}
}
