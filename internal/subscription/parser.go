package subscription

import (
	"strings"
	"time"
)

// maxLineFailures 失败明细条数上限:粘贴导入的内容可达 1MiB 垃圾行,
// 明细无界会灌爆响应体;计数(ParseFailures)始终精确,只截断明细。
const maxLineFailures = 200

// LineFailure 单行解析失败明细(手动机场导入结果逐行报告用)。
type LineFailure struct {
	Line   int    `json:"line"`   // 原文 1 起始行号(含空行计数,与用户编辑器行号对齐)
	Reason string `json:"reason"` // 解析错误摘要
}

// ParseResult contains parsing statistics.
type ParseResult struct {
	Nodes         []*Node
	TotalLines    int
	ParseFailures int
	// Failures 失败行明细(加法式附加;条数上限 maxLineFailures,ParseFailures 是全量计数)。
	Failures []LineFailure
}

// DedupeByNodeKey 行内去重(手动机场粘贴导入):同 NodeKey 后条覆盖前条,
// 沿用 NodeKey upsert 语义,不发明新规则。返回新切片,不修改入参。
func DedupeByNodeKey(nodes []*Node) []*Node {
	index := make(map[string]int, len(nodes))
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		key := n.NodeKey()
		if i, dup := index[key]; dup {
			out[i] = n // 后条覆盖前条(保留后条位置)
			continue
		}
		index[key] = len(out)
		out = append(out, n)
	}
	return out
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

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		result.TotalLines++

		node, err := f.parseNode(line, source)
		if err != nil {
			result.ParseFailures++
			if len(result.Failures) < maxLineFailures {
				result.Failures = append(result.Failures, LineFailure{Line: i + 1, Reason: err.Error()})
			}
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
