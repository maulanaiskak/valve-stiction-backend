// Backend service: REST API + WebSocket for the valve stiction dashboard.
// API-only -- valve-stiction-frontend is a separately built/deployed nginx
// image that reverse-proxies to this service (see its Dockerfile and
// docs/V3_PLAN.md's revision note). An earlier version had this service
// build and serve the frontend's static files directly, with the frontend
// pulled in via `git clone` at this image's build time -- reaching into
// another repo's source at build time couples the two services' build
// lifecycles together, which isn't how independently deployable services
// should work. Reverted.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write json response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": msg})
}

func handleSensors(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statuses, err := latestStatusPerSensor(r.Context(), db)
		if err != nil {
			log.Printf("latestStatusPerSensor failed: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to query sensor status")
			return
		}
		writeJSON(w, statuses)
	}
}

func handleSensorWindows(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sensorID := r.PathValue("id")
		if sensorID == "" {
			writeError(w, http.StatusBadRequest, "missing sensor id")
			return
		}

		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = parsed
		}

		windows, err := recentWindows(r.Context(), db, sensorID, limit)
		if err != nil {
			log.Printf("recentWindows failed: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to query windows")
			return
		}
		writeJSON(w, windows)
	}
}

func main() {
	dbURL := getenv("DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/valve_stiction")
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("failed to create db pool for %s: %v", dbURL, err)
	}
	defer db.Close()

	h := newHub(db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sensors", handleSensors(db))
	mux.HandleFunc("GET /api/sensors/{id}/windows", handleSensorWindows(db))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ws", h.serveWS)

	port := getenv("BACKEND_PORT", "8080")
	log.Printf("backend listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
