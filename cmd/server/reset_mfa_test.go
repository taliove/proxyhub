package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/store"
)

// fakeMFAStore records ResetUserMFA calls so tests can assert the reset hit the
// right account (and did not hit the database at all for unknown usernames).
type fakeMFAStore struct {
	users     map[string]*store.User
	lookupErr error
	resetErr  error
	resetIDs  []int64
}

func (f *fakeMFAStore) GetUserByUsername(username string) (*store.User, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	u, ok := f.users[username]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeMFAStore) ResetUserMFA(id int64) error {
	f.resetIDs = append(f.resetIDs, id)
	return f.resetErr
}

func newFakeMFAStore() *fakeMFAStore {
	return &fakeMFAStore{
		users: map[string]*store.User{
			"alice": {ID: 7, Username: "alice", Role: store.RoleSuperAdmin},
		},
	}
}

func TestResetMFAForUsername_ResetsAndConfirms(t *testing.T) {
	st := newFakeMFAStore()
	var out bytes.Buffer

	if err := resetMFAForUsername(st, "alice", &out); err != nil {
		t.Fatalf("resetMFAForUsername() error = %v", err)
	}

	if len(st.resetIDs) != 1 || st.resetIDs[0] != 7 {
		t.Errorf("resetIDs = %v, want [7]", st.resetIDs)
	}
	got := out.String()
	for _, want := range []string{"MFA reset for user", "alice", "id=7", "enrollment"} {
		if !strings.Contains(got, want) {
			t.Errorf("confirmation %q missing %q", got, want)
		}
	}
}

func TestResetMFAForUsername_UnknownUser(t *testing.T) {
	st := newFakeMFAStore()
	var out bytes.Buffer

	err := resetMFAForUsername(st, "ghost", &out)
	if err == nil {
		t.Fatal("unknown username expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no such user") {
		t.Errorf("error = %q, want it to mention 'no such user'", err)
	}
	if len(st.resetIDs) != 0 {
		t.Errorf("resetIDs = %v, want no reset for an unknown username", st.resetIDs)
	}
	if out.Len() != 0 {
		t.Errorf("out = %q, want no confirmation on failure", out.String())
	}
}

func TestResetMFAForUsername_ConcurrentDeleteReportedAsUnknown(t *testing.T) {
	st := newFakeMFAStore()
	st.resetErr = store.ErrNotFound
	var out bytes.Buffer

	err := resetMFAForUsername(st, "alice", &out)
	if err == nil {
		t.Fatal("reset returning ErrNotFound expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no such user") {
		t.Errorf("error = %q, want it to mention 'no such user'", err)
	}
}

func TestResetMFAForUsername_LookupErrorPropagates(t *testing.T) {
	st := newFakeMFAStore()
	st.lookupErr = errors.New("database is locked")
	var out bytes.Buffer

	err := resetMFAForUsername(st, "alice", &out)
	if err == nil {
		t.Fatal("lookup failure expected error, got nil")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("error = %q, want the underlying cause preserved", err)
	}
}

func TestResetMFAForUsername_ResetErrorPropagates(t *testing.T) {
	st := newFakeMFAStore()
	st.resetErr = errors.New("disk full")
	var out bytes.Buffer

	err := resetMFAForUsername(st, "alice", &out)
	if err == nil {
		t.Fatal("reset failure expected error, got nil")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %q, want the underlying cause preserved", err)
	}
	if out.Len() != 0 {
		t.Errorf("out = %q, want no confirmation on failure", out.String())
	}
}

func TestRunResetMFA_RequiresUsername(t *testing.T) {
	// No --username: must fail before touching the config or database, so the
	// missing config.yaml in the test working directory is not what fails.
	err := runResetMFA(nil, io.Discard)
	if err == nil {
		t.Fatal("missing --username expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--username") {
		t.Errorf("error = %q, want it to name the missing flag", err)
	}
}

func TestRunResetMFA_RejectsBlankUsername(t *testing.T) {
	err := runResetMFA([]string{"--username", "   "}, io.Discard)
	if err == nil {
		t.Fatal("blank --username expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--username") {
		t.Errorf("error = %q, want it to name the missing flag", err)
	}
}

func TestRunResetMFA_UnknownFlag(t *testing.T) {
	// flag.ContinueOnError prints usage to the flag set's output; the returned
	// error is what the caller turns into a non-zero exit.
	if err := runResetMFA([]string{"--nope"}, io.Discard); err == nil {
		t.Fatal("unknown flag expected error, got nil")
	}
}

func TestRunResetMFA_MissingConfig(t *testing.T) {
	err := runResetMFA([]string{
		"--username", "alice",
		"--config", "testdata/does-not-exist.yaml",
	}, io.Discard)
	if err == nil {
		t.Fatal("missing config expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("error = %q, want it to mention the config load failure", err)
	}
}

func TestRunSubcommand_Dispatch(t *testing.T) {
	// reset-mfa is dispatched (and fails on the missing flag), while an
	// unrelated first argument falls through to the service startup path.
	handled, err := runSubcommand("reset-mfa", nil)
	if !handled {
		t.Error("reset-mfa should be handled as a subcommand")
	}
	if err == nil {
		t.Error("reset-mfa without --username should return an error")
	}

	if handled, err := runSubcommand("--config", []string{"config.yaml"}); handled || err != nil {
		t.Errorf("runSubcommand(--config) = (%v, %v), want (false, nil)", handled, err)
	}
}
