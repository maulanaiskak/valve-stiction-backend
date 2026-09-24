// Package http is the REST delivery adapter -- translates HTTP requests
// into usecase calls and usecase results into JSON. No business logic and
// no DB access here.
package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/maulanaiskak/valve-stiction-backend/usecase"
)

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

func HandleSensors(svc *usecase.SensorService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statuses, err := svc.LatestStatuses(r.Context())
		if err != nil {
			log.Printf("LatestStatuses failed: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to query sensor status")
			return
		}
		writeJSON(w, statuses)
	}
}

func HandleSensorWindows(svc *usecase.SensorService) http.HandlerFunc {
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

		windows, err := svc.RecentWindows(r.Context(), sensorID, limit)
		if err != nil {
			log.Printf("RecentWindows failed: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to query windows")
			return
		}
		writeJSON(w, windows)
	}
}

func HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
