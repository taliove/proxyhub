package captcha

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestService builds a service with a deterministic clock so TTL and
// throttle windows can be advanced without sleeping.
func newTestService(now func() time.Time) *Service {
	return NewService(Options{Now: now})
}

// TestIssue_ReturnsChallengeAndImage covers the happy path: an issued
// challenge carries a non-empty id and a PNG data URL.
func TestIssue_ReturnsChallengeAndImage(t *testing.T) {
	svc := newTestService(time.Now)

	ch, err := svc.Issue("1.2.3.4")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if ch.ID == "" {
		t.Error("challenge id is empty")
	}
	if !strings.HasPrefix(ch.ImageBase64, "data:image/png;base64,") {
		t.Errorf("image prefix = %q, want PNG data URL", firstN(ch.ImageBase64, 32))
	}
	if len(ch.ImageBase64) < 512 {
		t.Errorf("image payload too small (%d bytes), noise/lines likely missing", len(ch.ImageBase64))
	}
}

// TestIssue_AnswerUsesExplicitCharset guards the confusable-character
// exclusion: answers may only contain ABCDEFGHJKMNPQRSTUVWXYZ23456789.
func TestIssue_AnswerUsesExplicitCharset(t *testing.T) {
	svc := newTestService(time.Now)

	// Spread over distinct IPs so the per-IP issue throttle stays out of the
	// way; this test is about the alphabet, not the rate limit.
	for i := 0; i < 200; i++ {
		ch, err := svc.Issue(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		answer := svc.peekAnswer(ch.ID)
		if len(answer) != CodeLength {
			t.Fatalf("answer %q length = %d, want %d", answer, len(answer), CodeLength)
		}
		for _, r := range answer {
			if !strings.ContainsRune(Charset, r) {
				t.Fatalf("answer %q contains %q outside charset %q", answer, r, Charset)
			}
		}
	}
}

// TestVerify_CaseInsensitiveAndTrimmed accepts the answer regardless of
// letter case and surrounding whitespace (users type either way).
func TestVerify_CaseInsensitiveAndTrimmed(t *testing.T) {
	svc := newTestService(time.Now)
	ch, _ := svc.Issue("1.2.3.4")
	answer := svc.peekAnswer(ch.ID)

	if !svc.Verify(ch.ID, "  "+strings.ToLower(answer)+" ") {
		t.Error("Verify() = false for lowercase/padded answer, want true")
	}
}

// TestVerify_WrongAnswerKeepsChallenge lets the user retry the same image
// after a typo, while a correct answer consumes the challenge.
func TestVerify_WrongAnswerKeepsChallenge(t *testing.T) {
	svc := newTestService(time.Now)
	ch, _ := svc.Issue("1.2.3.4")
	answer := svc.peekAnswer(ch.ID)

	if svc.Verify(ch.ID, "WRONG1") {
		t.Fatal("Verify() = true for wrong answer")
	}
	if !svc.Verify(ch.ID, answer) {
		t.Error("Verify() = false after a wrong attempt; challenge must survive typos")
	}
}

// TestVerify_SingleUse a challenge is consumed on success: replaying the
// same id and answer must fail.
func TestVerify_SingleUse(t *testing.T) {
	svc := newTestService(time.Now)
	ch, _ := svc.Issue("1.2.3.4")
	answer := svc.peekAnswer(ch.ID)

	if !svc.Verify(ch.ID, answer) {
		t.Fatal("first Verify() = false, want true")
	}
	if svc.Verify(ch.ID, answer) {
		t.Error("second Verify() = true, want false (challenge must be single-use)")
	}
}

// TestVerify_ConcurrentConsumeOnlyOnceSucceeds the delete must be atomic:
// racing goroutines submitting the same id+answer yield exactly one success.
func TestVerify_ConcurrentConsumeOnlyOnceSucceeds(t *testing.T) {
	svc := newTestService(time.Now)
	ch, _ := svc.Issue("1.2.3.4")
	answer := svc.peekAnswer(ch.ID)

	const racers = 32
	var wg sync.WaitGroup
	results := make([]bool, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx] = svc.Verify(ch.ID, answer)
		}(i)
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, ok := range results {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("concurrent Verify wins = %d, want exactly 1", wins)
	}
}

// TestVerify_ExpiredChallenge challenges die after TTL (5 minutes).
func TestVerify_ExpiredChallenge(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{now: now}
	svc := newTestService(clock.Now)

	ch, _ := svc.Issue("1.2.3.4")
	answer := svc.peekAnswer(ch.ID)

	clock.advance(TTL - time.Second)
	if !svc.Verify(ch.ID, answer) {
		t.Fatal("Verify() = false just before TTL, want true")
	}

	ch2, _ := svc.Issue("1.2.3.4")
	answer2 := svc.peekAnswer(ch2.ID)
	clock.advance(TTL + time.Second)
	if svc.Verify(ch2.ID, answer2) {
		t.Error("Verify() = true after TTL, want false")
	}
}

// TestVerify_UnknownID unknown or empty ids never verify.
func TestVerify_UnknownID(t *testing.T) {
	svc := newTestService(time.Now)
	if svc.Verify("", "ABCDEF") {
		t.Error("Verify(\"\") = true, want false")
	}
	if svc.Verify("no-such-id", "ABCDEF") {
		t.Error("Verify(unknown) = true, want false")
	}
}

// TestIssue_ThrottlePerIP allows IssueRateLimit issues per IP per minute;
// the next one is refused with ErrRateLimited.
func TestIssue_ThrottlePerIP(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	svc := newTestService(clock.Now)

	for i := 0; i < IssueRateLimit; i++ {
		if _, err := svc.Issue("5.5.5.5"); err != nil {
			t.Fatalf("Issue #%d error = %v, want nil", i+1, err)
		}
	}
	if _, err := svc.Issue("5.5.5.5"); err != ErrRateLimited {
		t.Errorf("Issue #%d error = %v, want ErrRateLimited", IssueRateLimit+1, err)
	}

	// A different IP has its own budget.
	if _, err := svc.Issue("6.6.6.6"); err != nil {
		t.Errorf("other IP Issue error = %v, want nil", err)
	}

	// The window is per minute: after it rolls over the budget resets.
	clock.advance(IssueRateWindow + time.Second)
	if _, err := svc.Issue("5.5.5.5"); err != nil {
		t.Errorf("Issue after window error = %v, want nil", err)
	}
}

// TestIssue_ThrottleConcurrent the throttle counter must not race: 2x the
// limit of concurrent issues still admits exactly IssueRateLimit.
func TestIssue_ThrottleConcurrent(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	svc := newTestService(clock.Now)

	const racers = IssueRateLimit * 2
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Issue("7.7.7.7"); err == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if admitted != IssueRateLimit {
		t.Errorf("admitted = %d, want %d", admitted, IssueRateLimit)
	}
}

// TestSweep_DropsExpired expired challenges must not accumulate: issuing
// after the TTL sweeps the dead entries.
func TestSweep_DropsExpired(t *testing.T) {
	clock := &fakeClock{now: time.Now()}
	svc := newTestService(clock.Now)

	for i := 0; i < 5; i++ {
		if _, err := svc.Issue("8.8.8.8"); err != nil {
			t.Fatalf("Issue error = %v", err)
		}
	}
	if got := svc.Pending(); got != 5 {
		t.Fatalf("Pending() = %d, want 5", got)
	}

	clock.advance(TTL + time.Second)
	if _, err := svc.Issue("8.8.8.9"); err != nil {
		t.Fatalf("Issue error = %v", err)
	}
	if got := svc.Pending(); got != 1 {
		t.Errorf("Pending() = %d after sweep, want 1", got)
	}
}

// fakeClock is a manually advanced clock for TTL and throttle assertions.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
