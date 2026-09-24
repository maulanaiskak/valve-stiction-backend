// Backend service: REST API + WebSocket for the valve stiction dashboard.
// API-only -- valve-stiction-frontend is a separately built/deployed nginx
// image that reverse-proxies to this service (see its Dockerfile and
// docs/V3_PLAN.md's revision note). An earlier version had this service
// build and serve the frontend's static files directly, with the frontend
// pulled in via `git clone` at this image's build time -- reaching into
// another repo's source at build time couples the two services' build
// lifecycles together, which isn't how independently deployable services
// should work. Reverted.
//
// This file is just wiring: env vars -> layered packages (domain,
// usecase, repository, delivery). All the actual logic lives there.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	httpdelivery "github.com/maulanaiskak/valve-stiction-backend/delivery/http"
	wsdelivery "github.com/maulanaiskak/valve-stiction-backend/delivery/ws"
	"github.com/maulanaiskak/valve-stiction-backend/repository"
	"github.com/maulanaiskak/valve-stiction-backend/usecase"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	dbURL := getenv("DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/valve_stiction")
	db, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("failed to create db pool for %s: %v", dbURL, err)
	}
	defer db.Close()

	svc := usecase.NewSensorService(repository.NewSensorRepo(db))

	hub := wsdelivery.NewHub(svc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sensors", httpdelivery.HandleSensors(svc))
	mux.HandleFunc("GET /api/sensors/{id}/windows", httpdelivery.HandleSensorWindows(svc))
	mux.HandleFunc("GET /healthz", httpdelivery.HandleHealthz)
	mux.HandleFunc("GET /ws", hub.ServeWS)

	port := getenv("BACKEND_PORT", "8080")
	log.Printf("backend listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
