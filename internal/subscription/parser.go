package subscription

import (
	"strings"
	"time"
)

// ParseResult contains parsing statistics.
type ParseResult struct {
	Nodes         []*Node
	TotalLines    int
	ParseFailures int
}

// ParseWithStats parses subscription content and returns nodes with statistics.
// Skips empty lines, metadata pseudo-nodes, and unparseable lines, counting failures.
func ParseWithStats(content, source string) *ParseResult {
	lines := strings.Split(content, "\n")
	result := &ParseResult{
		TotalLines: 0,
	}

	// Create a fetcher instance to reuse parsing logic
	f := NewFetcher(10 * time.Second)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		result.TotalLines++

		node, err := f.parseNode(line, source)
		if err != nil {
			result.ParseFailures++
			continue
		}

		// Skip metadata pseudo-nodes
		if isMetadataName(node.Name) {
			continue
		}

		// Preserve raw link
		node.RawLink = line
		result.Nodes = append(result.Nodes, node)
	}

	return result
}
