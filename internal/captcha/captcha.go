// Package captcha issues and verifies the login image captcha that guards
// POST /api/login once an IP has accumulated failed attempts.
//
// Design constraints (see docs/adr/0032-login-hardening-mfa-captcha.md):
//   - explicit charset without confusable glyphs (no 0/O/1/I/l);
//   - challenges live in memory only, 5 minute TTL, single use: a submission
//     consumes the challenge whether the answer was right or wrong;
//   - per-IP issue throttle so the endpoint cannot be used as a CPU sink.
//
// Losing challenges on restart is accepted: the TTL is short and the user
// simply requests a fresh image.
package captcha

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"

	base64Captcha "github.com/mojocn/base64Captcha"
)

const (
	// Charset is the answer alphabet: 31 glyphs with 0/O/1/I/l removed so a
	// user never has to guess which character they are looking at.
	Charset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

	// CodeLength is the number of characters drawn per challenge.
	CodeLength = 6

	// TTL is how long an unsolved challenge stays valid.
	TTL = 5 * time.Minute

	// IssueRateLimit caps how many challenges a single IP may request per
	// IssueRateWindow. Exceeding it yields ErrRateLimited (HTTP 429).
	IssueRateLimit = 30

	// IssueRateWindow is the throttle window for IssueRateLimit.
	IssueRateWindow = time.Minute

	// image geometry: wide enough for 6 glyphs plus noise.
	imageWidth  = 200
	imageHeight = 70
	// noiseCount is the number of stray glyphs scattered over the image.
	noiseCount = 6
)

// ErrRateLimited is returned by Issue when the caller IP exhausted its
// per-window budget.
var ErrRateLimited = errors.New("captcha issue rate limited")

// Challenge is what the HTTP layer hands to the client: an opaque id plus
// the rendered image as a PNG data URL. The answer never leaves the server.
type Challenge struct {
	ID          string `json:"challenge_id"`
	ImageBase64 string `json:"image_base64"`
}

// challenge is the server-side record for a pending challenge.
type challenge struct {
	answer    string
	expiresAt time.Time
}

// Options configures a Service. The zero value is valid: it uses the real
// clock, crypto/rand answers and the package default geometry.
type Options struct {
	// Now overrides the clock (tests advance it manually).
	Now func() time.Time
	// NewAnswer overrides answer generation. Production leaves it nil and
	// gets crypto/rand draws from Charset; tests inject a fixed answer so
	// they can solve a rendered challenge without OCR.
	NewAnswer func() (string, error)
}

// Service issues and verifies challenges. It is safe for concurrent use.
type Service struct {
	now       func() time.Time
	newAnswer func() (string, error)
	driver    base64Captcha.Driver

	mu         sync.Mutex
	challenges map[string]challenge
	// issues counts challenges handed out per IP inside the current window.
	issues map[string]*issueWindow
}

// issueWindow is a per-IP fixed-window counter for the issue throttle.
type issueWindow struct {
	count      int
	windowEnds time.Time
}

// NewService builds a Service with the fixed charset, geometry and the
// noise + interference line options required by the spec.
func NewService(opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	newAnswer := opts.NewAnswer
	if newAnswer == nil {
		newAnswer = randomAnswer
	}
	// Hollow + slime + sine lines all on: three independent interference
	// families make naive segmentation OCR unreliable.
	lineOptions := base64Captcha.OptionShowHollowLine |
		base64Captcha.OptionShowSlimeLine |
		base64Captcha.OptionShowSineLine
	driver := base64Captcha.NewDriverString(
		imageHeight, imageWidth, noiseCount, lineOptions,
		CodeLength, Charset, nil, nil, nil,
	)
	return &Service{
		now:        now,
		newAnswer:  newAnswer,
		driver:     driver,
		challenges: make(map[string]challenge),
		issues:     make(map[string]*issueWindow),
	}
}

// Issue renders a new challenge for ip. It returns ErrRateLimited when the
// IP already consumed IssueRateLimit challenges in the current window.
func (s *Service) Issue(ip string) (Challenge, error) {
	now := s.now()
	if !s.admitIssue(ip, now) {
		return Challenge{}, ErrRateLimited
	}

	answer, err := s.newAnswer()
	if err != nil {
		return Challenge{}, err
	}
	item, err := s.driver.DrawCaptcha(answer)
	if err != nil {
		return Challenge{}, err
	}
	id, err := randomID()
	if err != nil {
		return Challenge{}, err
	}

	s.mu.Lock()
	s.sweepLocked(now)
	s.challenges[id] = challenge{answer: answer, expiresAt: now.Add(TTL)}
	s.mu.Unlock()

	return Challenge{ID: id, ImageBase64: item.EncodeB64string()}, nil
}

// Verify checks answer against the challenge id. Submitting consumes the
// challenge atomically whether the answer was right or wrong (single use,
// concurrency safe): keeping a wrong-answered challenge alive would hand an
// attacker unlimited guesses against one image. A user who mistypes gets a
// fresh image instead. Expired or unknown ids always fail.
func (s *Service) Verify(id, answer string) bool {
	if id == "" {
		return false
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	ch, ok := s.challenges[id]
	if !ok {
		return false
	}
	delete(s.challenges, id)
	if !now.Before(ch.expiresAt) {
		return false
	}
	return answerMatches(ch.answer, answer)
}

// Pending reports how many live challenges are held. Used by tests and
// potentially by ops metrics; it also drops expired entries.
func (s *Service) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(s.now())
	return len(s.challenges)
}

// admitIssue applies the per-IP fixed-window throttle. Returns false when
// the budget for the current window is exhausted.
func (s *Service) admitIssue(ip string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.issues[ip]
	if !ok || !now.Before(w.windowEnds) {
		s.issues[ip] = &issueWindow{count: 1, windowEnds: now.Add(IssueRateWindow)}
		s.pruneWindowsLocked(now)
		return true
	}
	if w.count >= IssueRateLimit {
		return false
	}
	w.count++
	return true
}

// sweepLocked drops expired challenges. Caller must hold the mutex.
func (s *Service) sweepLocked(now time.Time) {
	for id, ch := range s.challenges {
		if !now.Before(ch.expiresAt) {
			delete(s.challenges, id)
		}
	}
}

// pruneWindowsLocked drops throttle counters whose window already closed so
// the map cannot grow without bound. Caller must hold the mutex.
func (s *Service) pruneWindowsLocked(now time.Time) {
	for ip, w := range s.issues {
		if !now.Before(w.windowEnds) {
			delete(s.issues, ip)
		}
	}
}

// answerMatches compares case-insensitively after trimming, in constant
// time for equal-length inputs (the answer is not a secret, but constant
// time comparison costs nothing here).
func answerMatches(want, got string) bool {
	got = strings.ToUpper(strings.TrimSpace(got))
	want = strings.ToUpper(strings.TrimSpace(want))
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// randomAnswer draws CodeLength characters from Charset using crypto/rand
// (base64Captcha's own generator uses math/rand, which is guessable).
func randomAnswer() (string, error) {
	max := big.NewInt(int64(len(Charset)))
	out := make([]byte, CodeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = Charset[n.Int64()]
	}
	return string(out), nil
}

// randomID returns an opaque 128-bit hex challenge id.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
