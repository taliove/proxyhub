package airporttest

import (
	"context"
	"errors"
	"fmt"

	"github.com/taliove/proxyhub/internal/store"
)

// StoreAdapter adapts store.Store to airporttest.Store interface.
type StoreAdapter struct {
	s *store.Store
}

// NewStoreAdapter creates a new store adapter.
func NewStoreAdapter(s *store.Store) *StoreAdapter {
	return &StoreAdapter{s: s}
}

// GetAirportURL 按 airport_id 现读订阅 URL(凭证不落 params_json);
// 机场已删(store.ErrNotFound)映射为 ErrAirportGone,不暴露底层细节。
func (a *StoreAdapter) GetAirportURL(_ context.Context, airportID int64) (string, error) {
	airport, err := a.s.GetAirportByID(airportID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrAirportGone
		}
		return "", fmt.Errorf("query airport: %w", err)
	}
	return airport.URL, nil
}

// CreateTestRun persists a test run.
func (a *StoreAdapter) CreateTestRun(ctx context.Context, run *TestRun) (int64, error) {
	storeRun := &store.AirportTestRun{
		AirportID:      run.AirportID,
		CreatedAt:      run.CreatedAt,
		SampleParams:   run.SampleParams,
		IsFull:         run.IsFull,
		Status:         string(run.Status),
		OverallScore:   run.OverallScore,
		DimensionsJSON: run.DimensionsJSON,
		ErrorMessage:   run.ErrorMessage,
		JobID:          run.JobID,
	}
	return a.s.CreateAirportTestRun(ctx, storeRun)
}

// GetTestRun retrieves a test run.
func (a *StoreAdapter) GetTestRun(ctx context.Context, airportID, runID int64) (*TestRun, error) {
	storeRun, err := a.s.GetAirportTestRun(ctx, airportID, runID)
	if err != nil {
		return nil, err
	}
	return &TestRun{
		ID:             storeRun.ID,
		AirportID:      storeRun.AirportID,
		CreatedAt:      storeRun.CreatedAt,
		SampleParams:   storeRun.SampleParams,
		IsFull:         storeRun.IsFull,
		Status:         RunStatus(storeRun.Status),
		OverallScore:   storeRun.OverallScore,
		DimensionsJSON: storeRun.DimensionsJSON,
		ErrorMessage:   storeRun.ErrorMessage,
		JobID:          storeRun.JobID,
	}, nil
}

// UpdateTestRun updates an existing test run.
func (a *StoreAdapter) UpdateTestRun(ctx context.Context, run *TestRun) error {
	storeRun := &store.AirportTestRun{
		ID:             run.ID,
		AirportID:      run.AirportID,
		CreatedAt:      run.CreatedAt,
		SampleParams:   run.SampleParams,
		IsFull:         run.IsFull,
		Status:         string(run.Status),
		OverallScore:   run.OverallScore,
		DimensionsJSON: run.DimensionsJSON,
		ErrorMessage:   run.ErrorMessage,
		JobID:          run.JobID,
	}
	return a.s.UpdateAirportTestRun(ctx, storeRun)
}
