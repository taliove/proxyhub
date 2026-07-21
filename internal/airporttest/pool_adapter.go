package airporttest

import (
	"fmt"

	"github.com/taliove/proxyhub/internal/geoip"
	"github.com/taliove/proxyhub/internal/store"
	"github.com/taliove/proxyhub/internal/subscription"
)

// StorePoolAdapter adapts store.Store to PoolOperations interface.
type StorePoolAdapter struct {
	store       *store.Store
	geoResolver *geoip.Resolver
}

// NewStorePoolAdapter creates a pool operations adapter wrapping store and geoip resolver.
func NewStorePoolAdapter(st *store.Store, geoResolver *geoip.Resolver) *StorePoolAdapter {
	return &StorePoolAdapter{
		store:       st,
		geoResolver: geoResolver,
	}
}

// LoadPoolBySource returns nodes from pool matching the given source (airport name).
func (a *StorePoolAdapter) LoadPoolBySource(source string) ([]*subscription.Node, error) {
	allNodes, err := a.store.LoadNodePool()
	if err != nil {
		return nil, fmt.Errorf("load node pool: %w", err)
	}

	var filtered []*subscription.Node
	for _, n := range allNodes {
		// Match by source, exclude stale nodes
		if n.Source == source && !n.Stale {
			filtered = append(filtered, n)
		}
	}
	return filtered, nil
}

// UpsertAirportNodes merges fetched nodes into pool (single-airport scope).
// Reuses global refresh merge logic: region recognition + MergePool + SaveNodePool.
func (a *StorePoolAdapter) UpsertAirportNodes(airportName string, fetchedNodes []*subscription.Node) error {
	// Step 1: Region recognition (reuse offline GeoIP if available)
	for _, node := range fetchedNodes {
		if node.Region == "" {
			// Try offline GeoIP lookup (non-blocking)
			if country, err := geoip.LookupCountry(node.Server); err == nil {
				node.Region = country
			}
		}
	}

	// Step 2: Load current pool
	oldPool, err := a.store.LoadNodePool()
	if err != nil {
		return fmt.Errorf("load old pool: %w", err)
	}

	// Step 3: Merge using MergePool (carry-forward detection state)
	// Filter old pool to only this airport's nodes to avoid touching others
	var airportOldPool []*subscription.Node
	var otherNodes []*subscription.Node
	for _, n := range oldPool {
		if n.Source == airportName {
			airportOldPool = append(airportOldPool, n)
		} else {
			otherNodes = append(otherNodes, n)
		}
	}

	mergedAirport := subscription.MergePool(airportOldPool, fetchedNodes)

	// Combine: merged airport nodes + untouched other nodes
	newPool := append(mergedAirport, otherNodes...)

	// Step 4: Save back to store
	if err := a.store.SaveNodePool(newPool); err != nil {
		return fmt.Errorf("save merged pool: %w", err)
	}

	return nil
}
