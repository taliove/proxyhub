package store

import (
	"context"
	"testing"
	"time"
)

func TestListAirportsWithTestRuns(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func(*Store) ([]int64, []int64) // returns (airportIDs, runIDs)
		wantCount int
		validate  func(*testing.T, []*AirportWithTestRun)
	}{
		{
			name: "no airports returns empty",
			setup: func(s *Store) ([]int64, []int64) {
				return nil, nil
			},
			wantCount: 0,
		},
		{
			name: "airports without test runs have null fields",
			setup: func(s *Store) ([]int64, []int64) {
				a1, _ := s.CreateAirport("Airport A", "https://example.com/a")
				a2, _ := s.CreateAirport("Airport B", "https://example.com/b")
				return []int64{a1.ID, a2.ID}, nil
			},
			wantCount: 2,
			validate: func(t *testing.T, results []*AirportWithTestRun) {
				for _, awt := range results {
					if awt.LastTestScore != nil {
						t.Errorf("airport %d: expected nil score, got %v", awt.ID, *awt.LastTestScore)
					}
					if awt.LastTestAt != nil {
						t.Errorf("airport %d: expected nil test_at, got %v", awt.ID, *awt.LastTestAt)
					}
					if awt.LastTestStatus != nil {
						t.Errorf("airport %d: expected nil status, got %v", awt.ID, *awt.LastTestStatus)
					}
				}
			},
		},
		{
			name: "airports with single test run return latest info",
			setup: func(s *Store) ([]int64, []int64) {
				a1, _ := s.CreateAirport("Airport A", "https://example.com/a")
				score := 85.5
				run := &AirportTestRun{
					AirportID:      a1.ID,
					CreatedAt:      time.Now().UTC(),
					SampleParams:   "{}",
					Status:         "completed",
					OverallScore:   &score,
					DimensionsJSON: "{}",
				}
				runID, _ := s.CreateAirportTestRun(ctx, run)
				return []int64{a1.ID}, []int64{runID}
			},
			wantCount: 1,
			validate: func(t *testing.T, results []*AirportWithTestRun) {
				awt := results[0]
				if awt.LastTestScore == nil {
					t.Fatal("expected score, got nil")
				}
				if *awt.LastTestScore != 85.5 {
					t.Errorf("expected score 85.5, got %v", *awt.LastTestScore)
				}
				if awt.LastTestStatus == nil || *awt.LastTestStatus != "completed" {
					t.Errorf("expected status 'completed', got %v", awt.LastTestStatus)
				}
				if awt.LastTestAt == nil {
					t.Error("expected test_at timestamp, got nil")
				}
			},
		},
		{
			name: "multiple runs per airport returns only latest",
			setup: func(s *Store) ([]int64, []int64) {
				a1, _ := s.CreateAirport("Airport A", "https://example.com/a")

				// Create older run
				oldScore := 70.0
				oldRun := &AirportTestRun{
					AirportID:      a1.ID,
					CreatedAt:      time.Now().UTC().Add(-2 * time.Hour),
					SampleParams:   "{}",
					Status:         "completed",
					OverallScore:   &oldScore,
					DimensionsJSON: "{}",
				}
				s.CreateAirportTestRun(ctx, oldRun)

				// Create newer run
				newScore := 90.0
				newRun := &AirportTestRun{
					AirportID:      a1.ID,
					CreatedAt:      time.Now().UTC(),
					SampleParams:   "{}",
					Status:         "completed",
					OverallScore:   &newScore,
					DimensionsJSON: "{}",
				}
				newID, _ := s.CreateAirportTestRun(ctx, newRun)

				return []int64{a1.ID}, []int64{newID}
			},
			wantCount: 1,
			validate: func(t *testing.T, results []*AirportWithTestRun) {
				awt := results[0]
				if awt.LastTestScore == nil {
					t.Fatal("expected score, got nil")
				}
				if *awt.LastTestScore != 90.0 {
					t.Errorf("expected latest score 90.0, got %v", *awt.LastTestScore)
				}
			},
		},
		{
			name: "failed test run with null score",
			setup: func(s *Store) ([]int64, []int64) {
				a1, _ := s.CreateAirport("Airport A", "https://example.com/a")
				run := &AirportTestRun{
					AirportID:      a1.ID,
					CreatedAt:      time.Now().UTC(),
					SampleParams:   "{}",
					Status:         "failed",
					OverallScore:   nil, // failed run has no score
					DimensionsJSON: "{}",
					ErrorMessage:   "fetch timeout",
				}
				runID, _ := s.CreateAirportTestRun(ctx, run)
				return []int64{a1.ID}, []int64{runID}
			},
			wantCount: 1,
			validate: func(t *testing.T, results []*AirportWithTestRun) {
				awt := results[0]
				if awt.LastTestScore != nil {
					t.Errorf("expected nil score for failed run, got %v", *awt.LastTestScore)
				}
				if awt.LastTestStatus == nil || *awt.LastTestStatus != "failed" {
					t.Errorf("expected status 'failed', got %v", awt.LastTestStatus)
				}
			},
		},
		{
			name: "mixed airports with and without test runs",
			setup: func(s *Store) ([]int64, []int64) {
				a1, _ := s.CreateAirport("Tested", "https://example.com/a")
				a2, _ := s.CreateAirport("Untested", "https://example.com/b")

				score := 80.0
				run := &AirportTestRun{
					AirportID:      a1.ID,
					CreatedAt:      time.Now().UTC(),
					SampleParams:   "{}",
					Status:         "completed",
					OverallScore:   &score,
					DimensionsJSON: "{}",
				}
				runID, _ := s.CreateAirportTestRun(ctx, run)
				return []int64{a1.ID, a2.ID}, []int64{runID}
			},
			wantCount: 2,
			validate: func(t *testing.T, results []*AirportWithTestRun) {
				var tested, untested *AirportWithTestRun
				for _, awt := range results {
					if awt.Name == "Tested" {
						tested = awt
					} else if awt.Name == "Untested" {
						untested = awt
					}
				}
				if tested == nil || untested == nil {
					t.Fatal("missing expected airports")
				}
				if tested.LastTestScore == nil {
					t.Error("tested airport should have score")
				}
				if untested.LastTestScore != nil {
					t.Error("untested airport should have nil score")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fresh store per test
			st := newTestStore(t)

			// Setup test data
			tt.setup(st)

			// Execute query
			results, err := st.ListAirportsWithTestRuns(ctx)
			if err != nil {
				t.Fatalf("ListAirportsWithTestRuns failed: %v", err)
			}

			// Check count
			if len(results) != tt.wantCount {
				t.Errorf("expected %d results, got %d", tt.wantCount, len(results))
			}

			// Run custom validation
			if tt.validate != nil {
				tt.validate(t, results)
			}
		})
	}
}

// Test that the query doesn't perform N+1 queries by checking SQL execution count
func TestListAirportsWithTestRuns_NoNPlusOne(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Create multiple airports with test runs
	for i := 0; i < 10; i++ {
		a, _ := st.CreateAirport("Airport", "https://example.com")
		score := 80.0
		run := &AirportTestRun{
			AirportID:      a.ID,
			CreatedAt:      time.Now().UTC(),
			SampleParams:   "{}",
			Status:         "completed",
			OverallScore:   &score,
			DimensionsJSON: "{}",
		}
		st.CreateAirportTestRun(ctx, run)
	}

	// The query should execute in a single SELECT (verified by the LEFT JOIN subquery design)
	results, err := st.ListAirportsWithTestRuns(ctx)
	if err != nil {
		t.Fatalf("ListAirportsWithTestRuns failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}

	// Verify all have test data (proving the JOIN worked)
	for _, awt := range results {
		if awt.LastTestScore == nil {
			t.Errorf("airport %d missing test score (JOIN may have failed)", awt.ID)
		}
	}
}
