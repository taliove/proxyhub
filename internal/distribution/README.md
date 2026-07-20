# Distribution Package

The distribution package orchestrates ProxyHub's traffic distribution feature, managing Xray-core configuration, process lifecycle, and statistics collection.

## Architecture

```
internal/distribution/
├── manager.go           # Core manager with lifecycle control
├── routing.go           # Xray routing rules builder
├── stats.go             # Traffic statistics collector
├── distribution_test.go # Integration tests
└── manager_test.go      # Unit tests
```

## Components

### Manager (`manager.go`)

Orchestrates the distribution feature lifecycle:
- **Start(ctx)** - Starts Xray if distribution is enabled
- **Stop()** - Gracefully stops Xray process
- **Reload(ctx, nodes)** - Regenerates config and restarts Xray
- **IsRunning()** - Checks Xray process status

### RoutingBuilder (`routing.go`)

Generates Xray-core JSON configuration:
- Resolves upstream nodes from NodeKey by matching against nodes pool
- Handles load balancing with multiple upstream nodes (balancer creation)
- Generates routing rules based on protocol:
  - **gRPC**: Match by serviceName field
  - **WebSocket**: Match by path field
- Returns error if upstream node not found in pool

### StatsCollector (`stats.go`)

Collects traffic statistics from Xray API:
- Runs in background goroutine with configurable interval
- Fetches stats from Xray API for each enabled path
- Records snapshots to `distribution_stats` table
- Updates cumulative totals in `distribution_paths` table
- Handles errors gracefully with logging

## Dependencies

- `internal/store` - Distribution config and persistence
- `internal/xray` - Process manager and stats client
- `internal/subscription` - Node type for upstream matching

## Usage Example

```go
import (
    "context"
    "log/slog"
    "time"
    
    "github.com/taliove/proxyhub/internal/distribution"
    "github.com/taliove/proxyhub/internal/store"
)

// Create manager
st, _ := store.Open("proxyhub.db")
logger := slog.Default()
mgr := distribution.NewManager(st, "xray", "/etc/xray/config.json", logger)

// Start distribution
ctx := context.Background()
if err := mgr.Start(ctx); err != nil {
    log.Fatal(err)
}
defer mgr.Stop()

// Reload with updated nodes
nodes := getNodesFromAggregator()
if err := mgr.Reload(ctx, nodes); err != nil {
    log.Error("reload failed", "error", err)
}

// Check status
if mgr.IsRunning() {
    log.Info("distribution is active")
}
```

## Testing

Run tests with coverage:
```bash
go test ./internal/distribution/... -v -cover
```

Current coverage: **80.1%** (meets 80% requirement)

## Key Design Decisions

1. **Immutability** - All config operations create new objects, never mutate existing ones
2. **Context-based lifecycle** - Background goroutines respect context cancellation
3. **Error handling** - Explicit error handling at every level with structured logging
4. **NodeKey matching** - Upstream nodes resolved by NodeKey (handles SNI-aware keys)
5. **Load balancing** - Multiple upstreams create Xray balancer with configurable strategy
6. **Stats collection** - Non-blocking with graceful error handling (logs warnings, continues)

## Protocol Support

### Supported Inbound Protocols
- VLESS
- VMess
- Trojan

### Supported Networks
- gRPC (with service name routing)
- WebSocket (with path routing)
- TCP (no path-based routing support)

### Load Balancing Strategies
- `random` - Random selection
- `round_robin` - Round-robin distribution
- `leastping` - Least latency selection
