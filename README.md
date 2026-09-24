# valve-stiction-backend

[![CI](https://github.com/maulanaiskak/valve-stiction-backend/actions/workflows/ci.yml/badge.svg)](https://github.com/maulanaiskak/valve-stiction-backend/actions)

REST + WebSocket backend for a distributed, real-time control-valve stiction detection pipeline. Reads detection results from TimescaleDB and serves them to the dashboard.

**Part of a 5-repo system, and the hub for its full documentation** — see [System Design (HLD)](docs/HLD.md) and [Whitepaper](docs/WHITEPAPER.md) below for the full picture: a train/serve model-generalization failure found, fixed, and honestly bounded; a monolith split into 5 independently-deployable services; 99.6%/AUC 0.9998 live-streaming detection accuracy after the fix.

| Repo | Role |
|---|---|
| [simulator](https://github.com/maulanaiskak/valve-stiction-simulator) | Synthetic PV/OP signal generator |
| [ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion) | MQTT subscribe, windowing, forwards to detection |
| [detection](https://github.com/maulanaiskak/valve-stiction-detection) | Classic detector + trained RF model |
| **backend** (this repo) | REST + WebSocket API |
| [frontend](https://github.com/maulanaiskak/valve-stiction-frontend) | React dashboard |

## System architecture

```mermaid
flowchart LR
    subgraph Edge
        SIM[Simulator]
    end
    MQ[[Mosquitto]]
    ING[Ingestion]
    DET["Detection\nclassic + RF"]
    DB[(TimescaleDB)]
    BE["Backend (this repo)\nREST + WebSocket"]
    FE[Frontend]

    SIM -->|MQTT| MQ --> ING
    ING -->|"gRPC (V1) / Kafka (V2)"| DET
    DET -->|result| ING
    ING -->|persist| DB
    DB --> BE
    BE -->|"/api, /ws"| FE
```

Full requirement traceability (FR/NFR), the ERD, sequence diagrams, and state diagrams for every service are in **[docs/HLD.md](docs/HLD.md)**.

## Where this service fits

```mermaid
sequenceDiagram
    autonumber
    participant DB as TimescaleDB
    participant BE as Backend (this repo)
    participant FE as Frontend

    loop every 1s
        BE->>DB: SELECT latest status
        BE-->>FE: WS push if changed
    end
    FE->>BE: GET recent windows
    BE->>DB: SELECT recent windows
    DB-->>BE: rows
    BE-->>FE: JSON
```

## Data model

```mermaid
erDiagram
    WINDOW_RESULTS {
        bigserial id PK
        text sensor_id
        timestamptz window_start
        text label "classic: yes / no / uncertain"
        double ellipse_index
        boolean kano_verdict
        text rf_label "RF: yes / no, nullable"
        double rf_probability "nullable"
        double_array pv
        double_array op
    }
```

## API

- `GET /api/sensors` — latest status per sensor.
- `GET /api/sensors/{id}/windows?limit=N` — recent raw PV/OP windows for a sensor.
- `GET /ws` — WebSocket; sends an initial snapshot, then a diff-based `update` message whenever a sensor's status changes. Polls every second (`PollInterval` in `delivery/ws/hub.go`) rather than any pub/sub — no shared channel exists between this and ingestion, and a 1s poll is simple and cheap enough at this data volume.
- `GET /healthz`

API-only — the frontend is a separately built/deployed nginx image that reverse-proxies to this service; this repo's build never touches the frontend's source. (An earlier version had this service clone and build the frontend at image-build time — reverted, see `docs/V3_PLAN.md`'s revision note; reaching into another repo's source during a build couples two services that should deploy independently.)

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

```bash
docker build -t valve-stiction-backend .
docker run -p 8080:8080 -e DATABASE_URL=... valve-stiction-backend
```

| Env var | Default |
|---|---|
| `DATABASE_URL` | `postgresql://postgres:postgres@localhost:5432/valve_stiction` |
| `BACKEND_PORT` | `8080` |

Run [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend) separately (its own container) pointed at this service via its `BACKEND_HOST` env var.

`db/init.sql` is a copy of the shared TimescaleDB schema (also kept in [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion) and [valve-stiction-detection](https://github.com/maulanaiskak/valve-stiction-detection) — no shared/orchestrator repo, so each service keeps its own copy).

## Testing

```bash
go test ./...
go vet ./...
```

## Documentation

Everything in `docs/` covers the whole 5-repo system, not just this service — kept here since this is where the pipeline's data ends up and the dashboard begins:

| Doc | Contents |
|---|---|
| [HLD.md](docs/HLD.md) | Functional/non-functional requirements, architecture, ERD, sequence diagrams, state diagrams, decision flowchart |
| [WHITEPAPER.md](docs/WHITEPAPER.md) | Full writeup: the RF model's train/serve failure, the fix, its honest boundary, and real-data verification — building on the author's undergraduate thesis |
| [STREAMING_EVALUATION.md](docs/STREAMING_EVALUATION.md) | Quantified classic-vs-RF evaluation against live-streamed windows, before and after the fix, on both synthetic and real data |
| [E2E_TEST.md](docs/E2E_TEST.md) | All 5 repos wired together manually and verified interoperating, no orchestrator |
| [V1_PLAN.md](docs/V1_PLAN.md) / [V2_PLAN.md](docs/V2_PLAN.md) / [V3_PLAN.md](docs/V3_PLAN.md) | Build-decision history from when this was one monorepo, before the 5-repo split |
