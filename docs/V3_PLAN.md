# V3 — Custom backend + frontend, RF model actually served

> Written when this project was one monorepo (`valve-stiction-pipeline`). Right after this was built and verified, the monorepo was split into [valve-stiction-simulator](https://github.com/maulanaiskak/valve-stiction-simulator), [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion), [valve-stiction-detection](https://github.com/maulanaiskak/valve-stiction-detection), [valve-stiction-backend](https://github.com/maulanaiskak/valve-stiction-backend) (this repo), and [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend) — one repo per service, no orchestrator repo tying them together (each service's README documents its own env vars and how it connects to the others). Kept here for the design history; file paths below refer to the old monorepo layout.

Status: done, then split into per-service repos (see note above). Scope: replace Grafana-as-primary-FE with a purpose-built
dashboard, and stop leaving the trained RF model (valve-stiction-ml) unused.

## Why

V1/V2 proved the pipeline end-to-end, but two gaps were flagged directly by
the author: (1) the trained RF model was never called anywhere in the live
system — only the classic detector (ellipse+Kano) ran; (2) Grafana can show
panels but can't do a "sticky valve" animation or the specific dashboard the
author wants for a portfolio demo. V3 addresses both.

## Target architecture

```
[Simulator] --MQTT--> [Go ingestion: window, call ML service, persist DB]
                              |  gRPC (WindowRequest/WindowResponse)
                              v
                     [Detection/ML service (Python): classic detector + RF,
                      stateless predictor, no DB access]
[Go ingestion] --> [TimescaleDB] <-- [Go backend: REST + WebSocket] --> [React/TS FE]
                                              (also serves the built FE's static files)
```

## Decisions

- **Detection service becomes a pure predictor.** `DetectionCore.detect()` no
  longer persists — it just returns a result. In gRPC mode (V1), the Go
  ingestion service persists after getting the response back, since it's the
  one with the synchronous round-trip. In Kafka mode (V2), there's no Go
  consumer downstream of Kafka, so `kafka_worker.py` keeps doing its own
  persistence after calling the same core — the shared core stays pure,
  persistence is each transport's own concern.
- **RF model shipped alongside the classic detector, not replacing it.**
  `detector.py` now also loads valve-stiction-ml's trained RF artifact and
  calls `predict_window` on every window, returning `rf_label`/
  `rf_probability` next to the existing `label`/`ellipse_index`/
  `kano_verdict`. Dashboard can show both; nothing forces picking one as "the"
  answer. Model artifact (`models/20260919_e5c9bf0/model.joblib`, ~2MB) is
  checked into this repo under `detection/model/model.joblib` — it doesn't
  exist anywhere the Docker build can otherwise reach (valve-stiction-ml
  gitignores `models/`), and 2MB is small enough to just commit.
- **ML model serving stays Python via gRPC, not ported to Go.** RF is a
  scikit-learn joblib artifact — Go can't load it without an ONNX conversion
  step (new dependency, new failure surface, unproven in this project).
  Keeping it in Python reuses the gRPC contract that's already built and
  tested; the "ML service" the author asked about is exactly the existing
  detection service, now stripped of DB responsibilities.
- **Go ingestion service gains DB writes.** Previously only the Python
  detection service touched TimescaleDB. Now `grpcPublisher.Publish` writes
  the row itself after the gRPC call returns, using `jackc/pgx`.
  `kafkaPublisher.Publish` is unaffected (V2 still persists in
  `kafka_worker.py`, see above).
- **New Go backend service (`backend/`), separate binary from ingestion.**
  Polls TimescaleDB (short interval, no new broker) for latest state, holds
  WebSocket connections, and pushes diffs. Chose polling over wiring a
  pub/sub for this because ingestion and backend are independent processes
  with no existing shared channel, and a 1s DB poll is simple, correct, and
  cheap at this data volume — not worth a new piece of infra (Redis/NATS)
  just to avoid it.
- **Same Go backend serves the built frontend's static files.** Matches the
  author's own phrasing ("go service untuk serve fe") literally: one Go
  binary serves `/api/*`, `/ws`, and the React app's static build output.
  Avoids a second web server (nginx) purely to host static files.
- **Frontend: React + TypeScript + Vite**, per the original PRD's V4 plan.
  Dashboard: per-sensor status, animated sticky-valve indicator (CSS,
  driven by `label`), PV-vs-OP phase plot (the same shape the ellipse
  detector scores), PV-over-time and OP-over-time line charts. WebSocket for
  live push, matching how the animation/status needs to update itself.
- **Grafana stays.** Kept as-is for ops/debugging (ad-hoc SQL against
  TimescaleDB); the new FE is the "front door" for demos, not a replacement.

## DB schema change

`window_results` gains `rf_label TEXT`, `rf_probability DOUBLE PRECISION`
(nullable — V2's Kafka path can lag behind on rolling this out, and old rows
from before this change won't have it).

## Verified end-to-end

`docker compose up --build`: ingestion windows, calls the ML service (gRPC),
gets back both classic and RF predictions, and persists them itself —
confirmed via `docker compose logs ingestion` showing `rf_label`/
`rf_probability` alongside the classic fields. `GET /api/sensors` and
`GET /api/sensors/{id}/windows` return live data from TimescaleDB. `/ws`
sends an initial snapshot then diff-based `update` messages as new windows
land (checked directly with a WebSocket client). `/` serves the built React
app and its hashed asset bundle (`/assets/*.js`, `*.css`) with 200s.

## Finding: RF doesn't generalize to the simulator's synthetic signal

Running live against the simulator (not held-out real data), the RF model
predicts `"yes"` with high confidence (0.86–0.99) for **every** sensor,
including `valve-2` (`STICTION_ENABLED=false`), which the classic detector
correctly calls `"no"`. This isn't a plumbing bug — feature extraction goes
through the same `valve_stiction_ml.features` code the model was trained
with (per-window z-score, then tsfel), so the RF is scoring exactly the
features it was trained to score. The gap is a genuine train/serve
distribution mismatch: RF was trained on ISDB/SACAC (real industrial
process data); the simulator's scripted triangle-wave OP + stick-slip valve
model has different noise/frequency characteristics it never saw. The
classic detector generalizes here because it's a fixed geometric/pattern
rule, not fit to any particular data distribution.

Left as-is, not "fixed": retraining on simulator output would be circular
(testing a model against the same distribution it was trained on proves
nothing), and the dashboard already surfaces both labels side by side —
letting a viewer see the disagreement directly is a more honest demo than
hiding it. Worth calling out explicitly in demos/writeups as a real
lesson about synthetic-data validation, not swept under the rug.
