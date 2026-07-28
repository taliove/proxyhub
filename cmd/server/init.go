package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/config"
	"github.com/taliove/proxyhub/internal/sitepath"
	"github.com/taliove/proxyhub/internal/store"
)

// init subcommand: the headless initialization channel used by install.sh.
// It performs the same persistence the POST /api/setup wizard does (admin
// credentials in settings KV + super-admin row in users + initialized flag)
// and additionally records the installer-supplied domain and Site Path, so a
// freshly installed service starts already initialized and never exposes the
// setup wizard.
//
// Usage:
//
//	printf '%s\n' "$PASSWORD" | proxyhub init \
//	  --config /etc/proxyhub/config.yaml --domain example.com \
//	  --username ph1a2b --site-path <20-64 chars> --password-stdin
//
// The password travels ONLY through stdin: never argv, `ps`, or shell history.
//
// Idempotency: install.sh refuses to re-run over a managed installation before
// reaching this command, so init treats an already-initialized system as an
// operator error and exits non-zero (same stance as handleSetup's 400).

// initPasswordMinLength mirrors the handleSetup policy (password >= 12 chars)
// so the CLI and the web wizard enforce the same floor.
const initPasswordMinLength = 12

// initDomainMaxLength mirrors the DNS hostname bound enforced by the
// installer's validate_domain (scripts/install/lib.sh).
const initDomainMaxLength = 253

// Ban-policy defaults written to the settings KV, mirroring
// internal/server/security.go (defaultBanThreshold / defaultBanDuration).
// The runtime already falls back to these values when the keys are absent;
// they are persisted here for parity with handleSetup and as an audit trail.
const (
	initBanThreshold = 5
	initBanDuration  = time.Hour
)

// domainSettingKey records the public domain serving the management UI.
// Written by init for disaster recovery / audit; no runtime consumer yet.
const domainSettingKey = "domain"

// initParams carries the validated init inputs.
type initParams struct {
	Domain   string
	Username string
	SitePath string
	Password string
}

// initStore is the store surface init needs. Narrowing it here keeps the
// command testable without a database file (same pattern as mfaStore).
type initStore interface {
	IsSystemInitialized() (bool, error)
	SaveSystemSettings(settings map[string]string) error
	GetUserByUsername(username string) (*store.User, error)
	CreateUser(username, passHash, role string, mustChangePassword bool) (*store.User, error)
	BackfillUserID() error
	SetSitePath(path string) error
	MarkSystemInitialized() error
}

// runInit parses the subcommand arguments, opens the database and initializes
// the system. stdin is the password channel; confirmation output goes to out.
func runInit(args []string, stdin io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", "config.yaml", "path to the config file")
	domain := fs.String("domain", "", "public domain serving the management UI (required)")
	username := fs.String("username", "", "super-admin username to create (required)")
	sitePath := fs.String("site-path", "", "Site Path prefix for the management UI (required)")
	passwordStdin := fs.Bool("password-stdin", false, "read the admin password from stdin (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	params, err := validateInitFlags(*domain, *username, *sitePath, *passwordStdin)
	if err != nil {
		return err
	}

	password, err := readInitPassword(stdin)
	if err != nil {
		return err
	}
	params.Password = password

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	return initSystem(st, params, out)
}

// validateInitFlags checks the flag inputs before any file or database is
// touched, so a malformed invocation fails fast and never creates state.
func validateInitFlags(domain, username, sitePath string, passwordStdin bool) (initParams, error) {
	if !passwordStdin {
		return initParams{}, errors.New("missing --password-stdin: the admin password is only accepted via stdin, never argv")
	}

	name := strings.TrimSpace(username)
	if name == "" {
		return initParams{}, errors.New("missing --username: specify the super-admin account to create")
	}
	if store.IsReservedUsername(name) {
		return initParams{}, fmt.Errorf("username %q is reserved and cannot be used", name)
	}

	d := strings.TrimSpace(domain)
	if d == "" {
		return initParams{}, errors.New("missing --domain: specify the public domain serving the management UI")
	}
	if len(d) > initDomainMaxLength {
		return initParams{}, fmt.Errorf("domain too long: %d characters, maximum %d", len(d), initDomainMaxLength)
	}
	if strings.ContainsAny(d, " \t\r\n") {
		return initParams{}, fmt.Errorf("domain %q contains whitespace", d)
	}

	sp := sitepath.Normalize(sitePath)
	if err := sitepath.Validate(sp); err != nil {
		return initParams{}, fmt.Errorf("invalid --site-path: %w", err)
	}

	return initParams{Domain: d, Username: name, SitePath: sp}, nil
}

// readInitPassword reads one line from r as the admin password. Only the
// trailing newline is stripped (the password itself may contain spaces), the
// same convention as readAuthenticationKey.
func readInitPassword(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", errors.New("empty password: pipe the admin password via stdin (printf '%s\\n' \"$PASSWORD\" | proxyhub init ... --password-stdin)")
	}
	if len(password) < initPasswordMinLength {
		return "", fmt.Errorf("password too short: %d characters, minimum %d", len(password), initPasswordMinLength)
	}
	return password, nil
}

// initSystem persists the initialized state: admin credentials in the
// settings KV (audit trail / migration source, as in handleSetup), the
// super-admin row in users, the domain and Site Path settings, and finally
// the initialized flag. An already-initialized system is an error, never a
// silent no-op.
func initSystem(st initStore, p initParams, out io.Writer) error {
	initialized, err := st.IsSystemInitialized()
	if err != nil {
		return fmt.Errorf("check initialization state: %w", err)
	}
	if initialized {
		return errors.New("system already initialized: refusing to overwrite the existing admin account")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	settings := map[string]string{
		"admin_user":      p.Username,
		"admin_pass_hash": string(hashed),
		"ban_threshold":   strconv.Itoa(initBanThreshold),
		"ban_duration":    initBanDuration.String(),
		domainSettingKey:  p.Domain,
	}
	if err := st.SaveSystemSettings(settings); err != nil {
		return fmt.Errorf("save system settings: %w", err)
	}

	// users 表是登录判定的事实来源(见 handleSetup 注释)。同名用户已存在
	// 而 initialized 标志缺失属于异常数据状态,沿用 setup 的处理:跳过创建,
	// 交给启动时的 MigrateAdminToSuperUser 兜底。
	createdUser := false
	if _, err := st.GetUserByUsername(p.Username); errors.Is(err, store.ErrNotFound) {
		if _, err := st.CreateUser(p.Username, string(hashed), store.RoleSuperAdmin, false); err != nil {
			return fmt.Errorf("create super-admin user: %w", err)
		}
		createdUser = true
	} else if err != nil {
		return fmt.Errorf("look up user %q: %w", p.Username, err)
	}
	if createdUser {
		if err := st.BackfillUserID(); err != nil {
			return fmt.Errorf("backfill user_id: %w", err)
		}
	}

	if err := st.SetSitePath(p.SitePath); err != nil {
		return fmt.Errorf("persist site path: %w", err)
	}

	if err := st.MarkSystemInitialized(); err != nil {
		return fmt.Errorf("mark system initialized: %w", err)
	}

	if _, err := fmt.Fprintf(out,
		"ProxyHub initialized: super-admin %q created, domain=%s, site-path=%s.\n"+
			"The web setup wizard is disabled; sign in at https://%s/%s/\n",
		p.Username, p.Domain, p.SitePath, p.Domain, p.SitePath); err != nil {
		return fmt.Errorf("write confirmation: %w", err)
	}
	return nil
}
