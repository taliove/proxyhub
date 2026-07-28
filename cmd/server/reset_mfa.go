package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/store"
)

// reset-mfa subcommand: the loopback self-rescue channel for an operator who
// lost the TOTP device (login-hardening ticket 07). It follows the
// state-fingerprint precedent: open the database directly, call the store
// method, print a machine-readable confirmation line.
//
// Reaching the box already means the highest privilege level (same philosophy
// as the SSH-tunnel escape hatch), so this command performs no further
// authentication. It is only reachable by a local shell, never over HTTP.
//
// Usage:
//
//	proxyhub reset-mfa --config /etc/proxyhub/config.yaml --username alice
//
// The account is returned to the never-enrolled state (secret dropped, TOTP
// disabled, recovery codes invalidated), so the next login walks the enrollment
// flow again. Unknown usernames exit non-zero with an explicit message: a
// silent success would leave the operator believing a typo'd account was
// rescued.

// mfaStore is the store surface reset-mfa needs. Narrowing it here keeps the
// command testable without a database file.
type mfaStore interface {
	GetUserByUsername(username string) (*store.User, error)
	ResetUserMFA(id int64) error
}

// runResetMFA parses the subcommand arguments, opens the database and clears
// the MFA enrollment of the requested account. Confirmation output goes to out.
func runResetMFA(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("reset-mfa", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "path to the config file")
	username := fs.String("username", "", "username whose MFA enrollment is cleared (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	name := strings.TrimSpace(*username)
	if name == "" {
		return errors.New("missing --username: specify the account whose MFA should be reset")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	return resetMFAForUsername(st, name, out)
}

// resetMFAForUsername clears the MFA enrollment of the named account.
// A missing account is an error, never a silent no-op.
func resetMFAForUsername(st mfaStore, username string, out io.Writer) error {
	user, err := st.GetUserByUsername(username)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no such user: %q", username)
	}
	if err != nil {
		return fmt.Errorf("look up user %q: %w", username, err)
	}

	if err := st.ResetUserMFA(user.ID); err != nil {
		// The account existed a moment ago; a not-found here means it was
		// deleted concurrently. Report it as such rather than as a raw
		// database error.
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no such user: %q", username)
		}
		return fmt.Errorf("reset mfa for %q: %w", username, err)
	}

	if _, err := fmt.Fprintf(out,
		"MFA reset for user %q (id=%d): TOTP secret dropped, TOTP disabled, recovery codes invalidated.\n"+
			"The next login for this account will require a fresh authenticator enrollment.\n",
		username, user.ID); err != nil {
		return fmt.Errorf("write confirmation: %w", err)
	}
	return nil
}
