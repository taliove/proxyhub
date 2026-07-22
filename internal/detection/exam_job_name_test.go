package detection

import (
	"context"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// 默认 kind 名保持 "exam"(兼容既有任务记录与取消分发)。
func TestExamKind_DefaultName(t *testing.T) {
	m := NewExamJobManager(
		func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport { return ExamReport{} },
		nil,
	)
	if got := m.kind.Name(); got != "exam" {
		t.Errorf("default kind Name() = %q, want %q", got, "exam")
	}
}

// WithExamKindName 覆盖 kind 名:同进程多套单发任务管理器并存时区分
// (如出网+稳定性单节点检查用 exam_stability,避免与完整体检的 exam 记录互相附加)。
func TestExamKind_NameOverride(t *testing.T) {
	m := NewExamJobManager(
		func(ctx context.Context, n *subscription.Node, emit func(ExamEvent)) ExamReport { return ExamReport{} },
		nil,
		WithExamKindName("exam_stability"),
	)
	if got := m.kind.Name(); got != "exam_stability" {
		t.Errorf("overridden kind Name() = %q, want %q", got, "exam_stability")
	}
}
