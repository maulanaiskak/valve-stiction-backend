CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS window_results (
    id             BIGSERIAL,
    sensor_id      TEXT NOT NULL,
    window_start   TIMESTAMPTZ NOT NULL,
    label          TEXT NOT NULL,
    ellipse_index  DOUBLE PRECISION NOT NULL,
    kano_verdict   BOOLEAN NOT NULL,
    rf_label       TEXT,                    -- nullable: RF model added in V3, older rows predate it
    rf_probability DOUBLE PRECISION,
    pv             DOUBLE PRECISION[] NOT NULL,
    op             DOUBLE PRECISION[] NOT NULL,
    PRIMARY KEY (id, window_start)
);

SELECT create_hypertable('window_results', 'window_start', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_window_results_sensor ON window_results (sensor_id, window_start DESC);
