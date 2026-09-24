// Package ws is the WebSocket delivery adapter: tracks connected clients
// and polls usecase.SensorService for changes to push. No DB access here
// -- that's usecase/repository's job.
//
// WS is the frontend's only data source -- both the snapshot sent on
// connect and every subsequent update carry the full sensor status *and*
// its latest window's pv/op, so the frontend never needs a REST call to
// render anything (the REST endpoints in delivery/http still exist and
// work, they're just not what the dashboard itself uses).
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

// sensorUpdate is the full WS payload for one sensor -- status plus its
// latest window -- used for both the initial snapshot and every
// subsequent update, so a client never has anything less than this.
type sensorUpdate struct {
	domain.SensorStatus
	PV []float64 `json:"pv,omitempty"`
	OP []float64 `json:"op,omitempty"`
}

// Hub tracks connected WebSocket clients and the last update it sent per
// sensor, so it only broadcasts when something actually changed, and so a
// newly-connecting client's snapshot is fully populated immediately.
type Hub struct {
	svc *usecase.SensorService

	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
	last    map[string]sensorUpdate // sensor_id -> last broadcast update
}

func NewHub(svc *usecase.SensorService) *Hub {
	return &Hub{
		svc:     svc,
		clients: make(map[*websocket.Conn]struct{}),
		last:    make(map[string]sensorUpdate),
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
	// send whatever we currently know immediately (including pv/op), so a
	// new client isn't stuck showing nothing until the next changed status.
	initial := make([]sensorUpdate, 0, len(h.last))
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

// buildUpdate attaches the sensor's latest window (pv/op) to its status.
func (h *Hub) buildUpdate(ctx context.Context, status domain.SensorStatus) sensorUpdate {
	full := sensorUpdate{SensorStatus: status}
	if windows, err := h.svc.RecentWindows(ctx, status.SensorID, 1); err == nil && len(windows) > 0 {
		full.PV = windows[0].PV
		full.OP = windows[0].OP
	} else if err != nil {
		log.Printf("[%s] failed to fetch latest window for WS push: %v", status.SensorID, err)
	}
	return full
}

func (h *Hub) broadcast(update sensorUpdate) {
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
				h.mu.Unlock()
				update := h.buildUpdate(ctx, s)
				h.mu.Lock()
				h.last[s.SensorID] = update
				h.mu.Unlock()
				h.broadcast(update)
				h.mu.Lock()
			}
			h.mu.Unlock()
		}
	}
}
