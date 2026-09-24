# V2 — Multi-sensor, distributed: build plan

> Written when this project was one monorepo (`valve-stiction-pipeline`), before it split into [valve-stiction-simulator](https://github.com/maulanaiskak/valve-stiction-simulator), [valve-stiction-ingestion](https://github.com/maulanaiskak/valve-stiction-ingestion), [valve-stiction-detection](https://github.com/maulanaiskak/valve-stiction-detection), [valve-stiction-backend](https://github.com/maulanaiskak/valve-stiction-backend) (this repo), and [valve-stiction-frontend](https://github.com/maulanaiskak/valve-stiction-frontend). Kept here for the design history; file paths below refer to the old monorepo layout.

Status: v1.0 (FR-7 through FR-10 all built and verified end-to-end, see below) · Scope: PRD §4 V2 (FR-7 through FR-10).

## Architecture (from PRD §6)

```
[Simulator: N valves] --MQTT--> [Mosquitto]
    --> [MQTT-Kafka bridge] --> [Kafka/Redpanda, partitioned by sensor_id]
    --> [Detection service x N instances, consumer group] --> [TimescaleDB] --> [Grafana]
```

## Key decision: V1 stays independently demoable

The PRD's own framing (§4): "each phase is independently 'done' and demoable." V2 must not break `docker compose up` giving exactly V1's working single-path demo. Rather than fork the codebase, the same binaries/images serve both:

- **Ingestion (Go)**: `PUBLISH_MODE` env var. `grpc` (default, unchanged V1 behavior) calls the detection service directly after windowing. `kafka` (V2) publishes each completed window to Redpanda instead, keyed by `sensor_id` for partitioning (FR-8) — the windowing logic itself (buffering, stride, per-sensor isolation) is identical in both modes, not duplicated.
- **Detection (Python)**: core detection logic (activity guard + classic detector + persistence) extracted into `detector.py`, shared by `main.py` (gRPC server, V1, unchanged) and `kafka_worker.py` (new, V2 — a Redpanda consumer). Running multiple `kafka_worker.py` instances in the same consumer group is what satisfies FR-9's horizontal scaling — Kafka/Redpanda's own partition assignment handles distributing sensor_id-partitioned work across instances, no extra code needed for that part.
- **Separate compose file, not profiles**: tried designing this as `docker compose --profile v2 up` additively bringing up Redpanda + kafka-mode ingestion alongside V1's services, but Compose profiles are additive, not a way to swap a service's config -- V1's `ingestion` (grpc mode) and a profile-gated `ingestion` (kafka mode) would both subscribe to MQTT and double-process every sample. Went with a complete, standalone `docker-compose.v2.yml` instead: `docker-compose.yml` (V1) stays untouched, `docker compose -f docker-compose.v2.yml up --build` runs the full V2 stack (redpanda, kafka-mode ingestion, N detection-worker replicas). Some duplication between the two files (mosquitto/timescaledb/grafana blocks repeat) -- an accepted, documented tradeoff for clarity over a merge-override that would need careful service removal semantics Compose doesn't cleanly support.

## Other decisions

- **Redpanda over Kafka** (PRD §11 open question, resolved): Kafka-API-compatible (same client library, same partitioning/consumer-group semantics as FR-8/FR-9 ask for), single-binary, lower overhead for local Compose dev.
- **Per-sensor MQTT topics** (FR-7, literal reading): simulator publishes to `valve/data/{sensor_id}` instead of V1's shared `valve/data` topic with sensor_id in the payload. Ingestion subscribes with a wildcard (`valve/data/+`). V1's load test already proved per-sensor isolation works with a shared topic + payload field; this change is about matching FR-7's literal architecture, not fixing a correctness gap.
- **Redpanda topic**: `valve-windows`, partitioned by `sensor_id` (matches FR-8 exactly). Message: same shape as the gRPC `WindowRequest` (sensor_id, pv[], op[], window_start_unix_ms), JSON-encoded (simpler than maintaining a second serialization path for the same proto over Kafka; revisit if a schema registry becomes worth it later).
- **Dashboard aggregation** (FR-10): add an "All sensors" table panel (latest label + timestamp per sensor_id) alongside V1's existing per-sensor detail view (already filterable via the `$sensor_id` template variable) — V1's dashboard already had the per-sensor piece, V2 adds the cross-sensor overview.

## Bugs found while wiring this up (not anticipated in the original plan)

- **Redpanda topic auto-creation was racy in practice.** `auto_create_topics_enabled` reports `true` on the broker, but ingestion's first several publishes still failed with "Unknown Topic Or Partition" — the topic didn't exist with propagated metadata yet by the time the producer's first write landed. Fixed with an explicit `redpanda-init` one-shot service (`docker-compose.v2.yml`) that retries `rpk topic create` until it succeeds, with `ingestion`/`detection-worker` depending on `service_completed_successfully` — deterministic instead of racing a lazy auto-create. Also gave the topic a fixed partition count (3, matching the planned replica count) rather than whatever a default auto-create would pick.
- **`rpk cluster health` needs the admin API, not the Kafka API** — tried it first for the init container's readiness check and it failed trying to reach `127.0.0.1:9644` regardless of `-X brokers=...` (that flag is for Kafka API calls, not the admin API). Simplified to retrying `rpk topic create` directly instead of checking cluster health as a separate step — it's actually closer to what matters here (is the Kafka API itself ready), not a workaround.
- **Zero log output from `kafka_worker.py` despite running correctly** — classic Python stdout block-buffering in a non-TTY context (Docker). `print()` calls were sitting in a buffer that `docker logs` never saw. Fixed with `ENV PYTHONUNBUFFERED=1` in `detection/Dockerfile` (and `simulator/Dockerfile`, same issue). Found this by checking `docker compose logs` came back completely empty for a container that `docker compose ps` showed as healthy and running — worth remembering as a diagnostic pattern (silent-but-running usually means buffering, not that nothing is happening).

## What's built vs. verified vs. deferred

Verified with `docker compose -f docker-compose.v2.yml up --build --scale detection-worker=3` (V1's stack stopped first -- both compose files share host ports 1883/5432/3000 by design, one demo runs at a time, see below):

- ✅ Redpanda running, topic `valve-windows` created with 3 partitions (matching the 3-replica scale-out)
- ✅ Ingestion (`PUBLISH_MODE=kafka`) subscribing to per-sensor MQTT topics (`valve/data/+`, FR-7) and publishing windows to Redpanda keyed by `sensor_id` (FR-8)
- ✅ 3 `detection-worker` replicas as one Kafka consumer group (`detection-group`) — **exactly the horizontal-scaling result FR-9 asks for**: each replica was assigned exactly one of the 3 partitions, and because `sensor_id` is the partition key, each partition consistently carried one sensor's windows the whole time (worker-1↔partition2↔valve-3, worker-2↔partition1↔valve-1, worker-3↔partition0↔valve-2). Correct labels per sensor (valve-1/valve-3 "yes", valve-2 "no", matching their configured stiction settings), confirmed both in worker logs and in `window_results` (14/14/14 rows, correct label per sensor).
- ✅ V1 stays independently demoable: `docker-compose.yml` untouched, re-verified working after every V2-driven refactor to shared code (`detector.py` extraction, ingestion's `windowPublisher` abstraction)
- ✅ Dashboard aggregation (FR-10): two new panels ahead of the existing per-sensor detail view (which stays filterable via `$sensor_id`, unchanged) -- "latest status" table (one row per sensor: label, last-seen timestamp, ellipse index) and a "stiction rate" bar chart (yes/no/uncertain window counts per sensor over the selected time range). Both verified directly against `/api/ds/query`: correct per-sensor attribution (valve-1/valve-3 mostly "yes", valve-2 mostly "no", matching their configured stiction settings), not just "the panel loaded."
- ⏳ Partition rebalancing under replica failure/scale-change not explicitly tested (e.g. killing one worker mid-stream and confirming its partition gets picked up by a survivor) — Kafka consumer groups handle this natively, but "the library does it" isn't the same as having watched it happen here
- **Deliberate, documented limitation**: V1 and V2 share host ports (1883 MQTT, 5432 Postgres, 3000 Grafana) since both compose files are meant to demo one phase at a time (`docker compose down` before switching), not run simultaneously. Giving V2 distinct ports was considered and skipped as unnecessary complexity for a portfolio scaffold.
