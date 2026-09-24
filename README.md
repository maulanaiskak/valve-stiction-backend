# valve-stiction-backend

REST + WebSocket backend for the valve stiction dashboard. Reads detection results from TimescaleDB (written by [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion)) and serves them to [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend). API-only — the frontend is a separately built/deployed nginx image that reverse-proxies to this service; this repo's build never touches the frontend's source. (An earlier version had this service clone and build the frontend at image-build time — reverted, see `docs/V3_PLAN.md`'s revision note; reaching into another repo's source during a build couples two services that should deploy independently.)

## API

- `GET /api/sensors` — latest status per sensor.
- `GET /api/sensors/{id}/windows?limit=N` — recent raw PV/OP windows for a sensor.
- `GET /ws` — WebSocket; sends an initial snapshot, then a diff-based `update` message whenever a sensor's status changes. Polls TimescaleDB every second (`PollInterval` in `ws.go`) rather than any pub/sub — no shared channel exists between this and ingestion, and a 1s poll is simple and cheap enough at this data volume.
- `GET /healthz`

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

`docs/` holds the build-decision docs (`V1_PLAN.md` through `V3_PLAN.md`) from when this whole pipeline was one monorepo, before it split into the five repos linked above. Kept here since this service is what all three phases eventually converge on.
