// Package usecase is what delivery adapters (HTTP, WebSocket) actually
// call -- they never touch repository directly, so the data-access layer
// can change without the transport layer knowing.
package usecase

import (
	"context"

	"github.com/maulanaiskak/valve-stiction-backend/domain"
	"github.com/maulanaiskak/valve-stiction-backend/repository"
)

type SensorService struct {
	repo *repository.SensorRepo
}

func NewSensorService(repo *repository.SensorRepo) *SensorService {
	return &SensorService{repo: repo}
}

func (s *SensorService) LatestStatuses(ctx context.Context) ([]domain.SensorStatus, error) {
	return s.repo.LatestStatusPerSensor(ctx)
}

func (s *SensorService) RecentWindows(ctx context.Context, sensorID string, limit int) ([]domain.WindowSample, error) {
	return s.repo.RecentWindows(ctx, sensorID, limit)
}
