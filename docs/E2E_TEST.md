# End-to-end test: 5 independent repos, no orchestrator

Verifies the claim behind the repo split: each of [valve-stiction-simulator](https://github.com/maulanaiskak/valve-stiction-simulator), [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion), [valve-stiction-detection](https://github.com/maulanaiskak/valve-stiction-detection), [valve-stiction-backend](https://github.com/maulanaiskak/valve-stiction-backend) (this repo), and [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend) builds and runs on its own, and the five interoperate correctly wired together by nothing but documented env vars and one cross-repo `git clone` (backend pulling frontend at Docker build time). No shared compose file, no submodules, no monorepo.

## Topology

```
[simulator ×2] --MQTT--> [Mosquitto]
    --> [ingestion] --gRPC--> [detection: classic detector + RF]
    --> [ingestion persists] --> [TimescaleDB]
    --> [backend: REST + WebSocket, serves frontend's build] --> [frontend]
```

All 5 service containers + Mosquitto + TimescaleDB run on one Docker network (`valve-e2e`), each built from its own repo's `Dockerfile`, wired only via env vars (`MQTT_BROKER_URL`, `DETECTION_SERVICE_ADDR`, `DATABASE_URL`, etc.) — the same env vars each repo's README documents for standalone use.

## What was built and run

| Step | Source | Result |
|---|---|---|
| TimescaleDB schema | `valve-stiction-backend/db/init.sql` (this repo's own copy) | `\dt` confirms `window_results` created |
| `detection` image | `valve-stiction-detection` repo, `docker build .` | gRPC server up on `:50051` |
| `ingestion` image | `valve-stiction-ingestion` repo, `docker build .` | connects to Mosquitto, subscribes `valve/data` |
| `backend` image | `valve-stiction-backend` repo, `docker build .` | Dockerfile clones `valve-stiction-frontend` from GitHub live during the build, then builds it — **this is the one step that actually crosses repos**, and it worked against the real, currently-pushed frontend repo, not a local copy |
| `simulator` image (×2) | `valve-stiction-simulator` repo, `docker build .` | one sensor with `STICTION_ENABLED=true` (`valve-1`), one `false` (`valve-2`) |

## Verified

1. **Simulator → Mosquitto → ingestion**: `ingestion` logs show windows arriving and being forwarded within seconds of the simulators starting.
2. **ingestion → detection → ingestion (persist)**: ingestion log line for `valve-1`:
   ```
   [valve-1] label=yes ellipse_index=1.900 kano=true has_activity=true rf_label=yes rf_probability=0.889
   ```
   Confirms detection returned both the classic verdict and the RF prediction over gRPC, and ingestion (not detection) is the one logging/persisting — matching the V3 design (detection is a stateless predictor now).
3. **ingestion → TimescaleDB → backend**: `GET /api/sensors` returned both sensors with data matching what ingestion logged, sourced independently through the DB — ingestion and backend never talk to each other directly:
   ```json
   [
     {"sensor_id": "valve-1", "label": "yes", "rf_label": "yes", "rf_probability": 0.90, ...},
     {"sensor_id": "valve-2", "label": "no",  "rf_label": "yes", "rf_probability": 0.95, ...}
   ]
   ```
4. **backend WebSocket**: a raw WS client connecting to `/ws` received an initial `snapshot` (both sensors) followed by a live `update` message as a new window landed — confirmed diff-based push works, not just the REST path.
5. **backend serving frontend**: `GET /` returned the built `<title>Valve Stiction Dashboard</title>` page, and its hashed JS bundle (`/assets/index-*.js`) returned `200` — the frontend cloned and built during the backend's own Docker build is the one actually being served, not a stale local copy.

## Finding reconfirmed

`valve-2` (`STICTION_ENABLED=false`) was correctly scored `label=no` by the classic detector but `rf_label=yes` (0.95 probability) by the RF model — same train/serve distribution mismatch documented in `V3_PLAN.md`. Reproduced here independently of the original monorepo test, so it's not an artifact of that specific run.

## Cleanup

All test containers (`e2e-*`) and the `valve-e2e` network were removed after verification — this was a manual wiring exercise to prove the repos are genuinely independent, not a deployment. Each repo's own README has the run instructions for actual local development.
