package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SensorStatus is one sensor's most recent detection result -- what the
// dashboard's status/animation/label needs. Matches window_results'
// columns except pv/op (fetched separately, only when a chart needs them --
// see WindowSample below).
type SensorStatus struct {
	SensorID      string    `json:"sensor_id"`
	WindowStart   time.Time `json:"window_start"`
	Label         string    `json:"label"`
	EllipseIndex  float64   `json:"ellipse_index"`
	KanoVerdict   bool      `json:"kano_verdict"`
	RFLabel       *string   `json:"rf_label"`
	RFProbability *float64  `json:"rf_probability"`
}

// WindowSample is one window's raw PV/OP series -- what the PV-vs-OP phase
// plot and the PV/OP-over-time charts need.
type WindowSample struct {
	WindowStart time.Time `json:"window_start"`
	Label       string    `json:"label"`
	PV          []float64 `json:"pv"`
	OP          []float64 `json:"op"`
}

func latestStatusPerSensor(ctx context.Context, db *pgxpool.Pool) ([]SensorStatus, error) {
	rows, err := db.Query(ctx, `
		SELECT DISTINCT ON (sensor_id)
			sensor_id, window_start, label, ellipse_index, kano_verdict, rf_label, rf_probability
		FROM window_results
		ORDER BY sensor_id, window_start DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SensorStatus
	for rows.Next() {
		var s SensorStatus
		if err := rows.Scan(
			&s.SensorID, &s.WindowStart, &s.Label, &s.EllipseIndex, &s.KanoVerdict,
			&s.RFLabel, &s.RFProbability,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func recentWindows(ctx context.Context, db *pgxpool.Pool, sensorID string, limit int) ([]WindowSample, error) {
	rows, err := db.Query(ctx, `
		SELECT window_start, label, pv, op
		FROM window_results
		WHERE sensor_id = $1
		ORDER BY window_start DESC
		LIMIT $2
	`, sensorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WindowSample
	for rows.Next() {
		var w WindowSample
		if err := rows.Scan(&w.WindowStart, &w.Label, &w.PV, &w.OP); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// DB returns newest-first (for LIMIT to keep the most recent N); charts
	// want chronological order.
	reverseInPlace(out)
	return out, nil
}

func reverseInPlace(out []WindowSample) {
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
}
