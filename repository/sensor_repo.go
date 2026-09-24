// Package repository holds TimescaleDB reads. No business logic here --
// that's usecase's job; this package just knows how to turn SQL rows into
// domain types.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/maulanaiskak/valve-stiction-backend/domain"
)

type SensorRepo struct {
	db *pgxpool.Pool
}

func NewSensorRepo(db *pgxpool.Pool) *SensorRepo {
	return &SensorRepo{db: db}
}

func (r *SensorRepo) LatestStatusPerSensor(ctx context.Context) ([]domain.SensorStatus, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT ON (sensor_id)
			sensor_id, window_start, label, ellipse_index, kano_verdict, rf_label, rf_probability
		FROM window_results
		ORDER BY sensor_id, window_start DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SensorStatus
	for rows.Next() {
		var s domain.SensorStatus
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

func (r *SensorRepo) RecentWindows(ctx context.Context, sensorID string, limit int) ([]domain.WindowSample, error) {
	rows, err := r.db.Query(ctx, `
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

	var out []domain.WindowSample
	for rows.Next() {
		var w domain.WindowSample
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

func reverseInPlace(out []domain.WindowSample) {
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
}
