package airporttest

import (
	"context"

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
	}
	return a.s.UpdateAirportTestRun(ctx, storeRun)
}
