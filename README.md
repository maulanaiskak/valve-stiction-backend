# valve-stiction-backend

REST + WebSocket backend for the valve stiction dashboard. Reads detection results from TimescaleDB (written by [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion)) and serves them to [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend). API-only — the frontend is a separately built/deployed nginx image that reverse-proxies to this service; this repo's build never touches the frontend's source. (An earlier version had this service clone and build the frontend at image-build time — reverted, see `docs/V3_PLAN.md`'s revision note; reaching into another repo's source during a build couples two services that should deploy independently.)

## API

- `GET /api/sensors` — latest status per sensor.
- `GET /api/sensors/{id}/windows?limit=N` — recent raw PV/OP windows for a sensor.
- `GET /ws` — WebSocket; sends an initial snapshot, then a diff-based `update` message whenever a sensor's status changes. Polls every second (`PollInterval` in `delivery/ws/hub.go`) rather than any pub/sub — no shared channel exists between this and ingestion, and a 1s poll is simple and cheap enough at this data volume.
- `GET /healthz`

## Architecture

Layered: `domain` (plain types, no I/O) → `usecase` (a thin service delivery adapters call — never touch `repository` directly) → `repository` (TimescaleDB reads) → `delivery` (REST and WebSocket adapters). `main.go` is just wiring.

```
domain/sensor.go              SensorStatus, WindowSample -- plain data
repository/sensor_repo.go      TimescaleDB reads
usecase/sensor_service.go      thin pass-through -- delivery calls this, not repository directly
delivery/http/handlers.go      REST: /api/sensors, /api/sensors/{id}/windows, /healthz
delivery/ws/hub.go             WebSocket: connection tracking + poll-and-diff broadcast
```

## Run

```bash
go build -o backend .
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/valve_stiction ./backend
```

| Env var | Default |
|---|---|
| `DATABASE_URL` | `postgresql://postgres:postgres@localhost:5432/valve_stiction` |
| `BACKEND_PORT` | `8080` |

Run [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend) separately (its own container) pointed at this service via its `BACKEND_HOST` env var.

`db/init.sql` is a copy of the shared TimescaleDB schema (also kept in [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion) and [valve-stiction-detection](https://github.com/maulanaiskak/valve-stiction-detection) — no shared/orchestrator repo, so each service keeps its own copy).

## Design history

`docs/` holds the build-decision docs (`V1_PLAN.md` through `V3_PLAN.md`) from when this whole pipeline was one monorepo, before it split into the five repos linked above, plus system-wide reference docs that cover all five: `HLD.md` (requirements, architecture, ERD, sequence/state diagrams, decision flowchart), `E2E_TEST.md` (all 5 repos wired together and verified working), `STREAMING_EVALUATION.md` (a quantified evaluation — classic detector vs. RF model — against 267 live-streamed windows), and `WHITEPAPER.md` (the full writeup, building on the author's undergraduate thesis this project is based on).
