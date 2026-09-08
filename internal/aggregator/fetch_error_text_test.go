package aggregator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// fetchErrorText(issue #143):订阅过大给中文明确文案,其余错误透传原始串。
func TestFetchErrorText(t *testing.T) {
	tooLarge := fmt.Errorf("read subscription body: %w: 33554433 bytes over 33554432 limit", subscription.ErrSubscriptionTooLarge)
	if got := fetchErrorText(tooLarge); !strings.Contains(got, "订阅过大") {
		t.Fatalf("expected 订阅过大 message, got %q", got)
	}

	other := errors.New("connection refused")
	if got := fetchErrorText(other); got != "connection refused" {
		t.Fatalf("expected raw error passthrough, got %q", got)
	}
}
