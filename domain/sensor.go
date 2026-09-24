// Package domain holds plain data types shared across layers -- no I/O.
package domain

import "time"

// SensorStatus is one sensor's most recent detection result -- what the
// dashboard's status/animation/label needs.
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
