package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/taliove/proxyhub/internal/store"
)

// fakeInitStore records init calls so tests can assert persistence order and
// error propagation without a database file.
type fakeInitStore struct {
	initialized  bool
	initializedE error
	settings     map[string]string
	settingsErr  error
	users        map[string]*store.User
	lookupErr    error
	createErr    error
	created      []string
	backfilled   bool
	backfillErr  error
	sitePath     string
	sitePathErr  error
	marked       bool
	markErr      error
}

func (f *fakeInitStore) IsSystemInitialized() (bool, error) {
	return f.initialized, f.initializedE
}

func (f *fakeInitStore) SaveSystemSettings(settings map[string]string) error {
	if f.settingsErr != nil {
		return f.settingsErr
	}
	f.settings = settings
	return nil
}

func (f *fakeInitStore) GetUserByUsername(username string) (*store.User, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	u, ok := f.users[username]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeInitStore) CreateUser(username, passHash, role string, mustChangePassword bool) (*store.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, username)
	u := &store.User{ID: int64(len(f.created)), Username: username, PassHash: passHash, Role: role}
	f.users[username] = u
	return u, nil
}

func (f *fakeInitStore) BackfillUserID() error {
	f.backfilled = true
	return f.backfillErr
}

func (f *fakeInitStore) SetSitePath(path string) error {
	if f.sitePathErr != nil {
		return f.sitePathErr
	}
	f.sitePath = path
	return nil
}

func (f *fakeInitStore) MarkSystemInitialized() error {
	if f.markErr != nil {
		return f.markErr
	}
	f.marked = true
	return nil
}

func newFakeInitStore() *fakeInitStore {
	return &fakeInitStore{users: map[string]*store.User{}}
}

// testSitePath satisfies internal/sitepath rules: 20-64 chars, 3+ character
// classes (lower/upper/digit/separator), not reserved.
const testSitePath = "Xk9-Qm2vLp7Rt4YwZ8aB"

func testInitParams() initParams {
	return initParams{
		Domain:   "proxy.example.com",
		Username: "ph1a2b",
		SitePath: testSitePath,
		Password: "correct horse battery",
	}
}

func TestInitSystem_PersistsAdminSettingsAndMarksInitialized(t *testing.T) {
	st := newFakeInitStore()
	var out bytes.Buffer

	if err := initSystem(st, testInitParams(), &out); err != nil {
		t.Fatalf("initSystem() error = %v", err)
	}

	if len(st.created) != 1 || st.created[0] != "ph1a2b" {
		t.Errorf("created = %v, want [ph1a2b]", st.created)
	}
	u := st.users["ph1a2b"]
	if u.Role != store.RoleSuperAdmin {
		t.Errorf("role = %q, want %q", u.Role, store.RoleSuperAdmin)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PassHash), []byte("correct horse battery")); err != nil {
		t.Errorf("stored hash does not match password: %v", err)
	}
	if !st.backfilled {
		t.Error("BackfillUserID was not called after creating the super-admin")
	}
	for _, key := range []string{"admin_user", "admin_pass_hash", "ban_threshold", "ban_duration", "domain"} {
		if st.settings[key] == "" {
			t.Errorf("settings[%q] missing", key)
		}
	}
	if st.settings["admin_user"] != "ph1a2b" {
		t.Errorf("settings[admin_user] = %q, want ph1a2b", st.settings["admin_user"])
	}
	if st.settings["domain"] != "proxy.example.com" {
		t.Errorf("settings[domain] = %q, want proxy.example.com", st.settings["domain"])
	}
	if st.sitePath != testSitePath {
		t.Errorf("sitePath = %q, want %q", st.sitePath, testSitePath)
	}
	if !st.marked {
		t.Error("MarkSystemInitialized was not called")
	}
	got := out.String()
	for _, want := range []string{"ph1a2b", "proxy.example.com", testSitePath} {
		if !strings.Contains(got, want) {
			t.Errorf("confirmation %q missing %q", got, want)
		}
	}
}

func TestInitSystem_AlreadyInitializedFails(t *testing.T) {
	st := newFakeInitStore()
	st.initialized = true
	var out bytes.Buffer

	err := initSystem(st, testInitParams(), &out)
	if err == nil {
		t.Fatal("already-initialized system expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("error = %q, want it to mention 'already initialized'", err)
	}
	if len(st.created) != 0 || st.marked {
		t.Errorf("no state should change on an initialized system: created=%v marked=%v", st.created, st.marked)
	}
	if out.Len() != 0 {
		t.Errorf("out = %q, want no confirmation on failure", out.String())
	}
}

func TestInitSystem_ExistingUserSkipsCreate(t *testing.T) {
	st := newFakeInitStore()
	st.users["ph1a2b"] = &store.User{ID: 9, Username: "ph1a2b", Role: store.RoleSuperAdmin}
	var out bytes.Buffer

	if err := initSystem(st, testInitParams(), &out); err != nil {
		t.Fatalf("initSystem() error = %v", err)
	}
	if len(st.created) != 0 {
		t.Errorf("created = %v, want no creation when the user already exists", st.created)
	}
	if st.backfilled {
		t.Error("BackfillUserID should only run after an actual create")
	}
	if !st.marked {
		t.Error("MarkSystemInitialized was not called")
	}
}

func TestInitSystem_ErrorsPropagate(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*fakeInitStore)
		want    string
	}{
		{"initialized check", func(f *fakeInitStore) { f.initializedE = errors.New("database is locked") }, "database is locked"},
		{"save settings", func(f *fakeInitStore) { f.settingsErr = errors.New("disk full") }, "disk full"},
		{"lookup user", func(f *fakeInitStore) { f.lookupErr = errors.New("schema mismatch") }, "schema mismatch"},
		{"create user", func(f *fakeInitStore) { f.createErr = errors.New("unique constraint") }, "unique constraint"},
		{"backfill", func(f *fakeInitStore) { f.backfillErr = errors.New("foreign key") }, "foreign key"},
		{"site path", func(f *fakeInitStore) { f.sitePathErr = errors.New("reserved word") }, "reserved word"},
		{"mark initialized", func(f *fakeInitStore) { f.markErr = errors.New("read-only") }, "read-only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeInitStore()
			tc.prepare(st)
			var out bytes.Buffer
			err := initSystem(st, testInitParams(), &out)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want the underlying cause %q preserved", err, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("out = %q, want no confirmation on failure", out.String())
			}
		})
	}
}

func TestValidateInitFlags(t *testing.T) {
	cases := []struct {
		name         string
		domain       string
		username     string
		sitePath     string
		passwordFlag bool
		wantErr      string
	}{
		{"missing --password-stdin", "proxy.example.com", "ph1a2b", testSitePath, false, "--password-stdin"},
		{"missing --username", "proxy.example.com", "", testSitePath, true, "--username"},
		{"blank --username", "proxy.example.com", "   ", testSitePath, true, "--username"},
		{"reserved username", "proxy.example.com", "admin", testSitePath, true, "reserved"},
		{"reserved username case-insensitive", "proxy.example.com", "Root", testSitePath, true, "reserved"},
		{"missing --domain", "", "ph1a2b", testSitePath, true, "--domain"},
		{"domain with whitespace", "proxy .example.com", "ph1a2b", testSitePath, true, "whitespace"},
		{"site path too short", "proxy.example.com", "ph1a2b", "Short-1A", true, "--site-path"},
		{"site path reserved word", "proxy.example.com", "ph1a2b", "setup", true, "--site-path"},
		{"site path bad charset", "proxy.example.com", "ph1a2b", "has space and padding", true, "--site-path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateInitFlags(tc.domain, tc.username, tc.sitePath, tc.passwordFlag)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateInitFlags_Normalizes(t *testing.T) {
	p, err := validateInitFlags(" proxy.example.com ", " ph1a2b ", "/"+testSitePath+"/", true)
	if err != nil {
		t.Fatalf("validateInitFlags() error = %v", err)
	}
	if p.Domain != "proxy.example.com" || p.Username != "ph1a2b" || p.SitePath != testSitePath {
		t.Errorf("params = %+v, want trimmed/normalized values", p)
	}
}

func TestReadInitPassword(t *testing.T) {
	t.Run("reads one line and strips newline", func(t *testing.T) {
		pw, err := readInitPassword(strings.NewReader("correct horse battery\n"))
		if err != nil {
			t.Fatalf("readInitPassword() error = %v", err)
		}
		if pw != "correct horse battery" {
			t.Errorf("password = %q", pw)
		}
	})
	t.Run("keeps inner and trailing spaces", func(t *testing.T) {
		pw, err := readInitPassword(strings.NewReader("space padded   \n"))
		if err != nil {
			t.Fatalf("readInitPassword() error = %v", err)
		}
		if pw != "space padded   " {
			t.Errorf("password = %q, want trailing spaces preserved", pw)
		}
	})
	t.Run("empty rejected", func(t *testing.T) {
		if _, err := readInitPassword(strings.NewReader("\n")); err == nil {
			t.Fatal("empty password expected error, got nil")
		}
	})
	t.Run("eof without newline accepted", func(t *testing.T) {
		pw, err := readInitPassword(strings.NewReader("correct horse battery"))
		if err != nil {
			t.Fatalf("readInitPassword() error = %v", err)
		}
		if pw != "correct horse battery" {
			t.Errorf("password = %q", pw)
		}
	})
	t.Run("too short rejected", func(t *testing.T) {
		_, err := readInitPassword(strings.NewReader("short\n"))
		if err == nil {
			t.Fatal("short password expected error, got nil")
		}
		if !strings.Contains(err.Error(), "too short") {
			t.Errorf("error = %q, want it to mention 'too short'", err)
		}
	})
}

// writeInitConfig materializes a config file mirroring what install.sh's
// _write_config produces, with the storage path redirected into dir.
func writeInitConfig(t *testing.T, dir string) string {
	t.Helper()
	content := fmt.Sprintf(`server:
  host: "127.0.0.1"
  port: 8080
storage:
  path: "%s"
health_check:
  interval: 15m
  latency_threshold: 500
  test_url: "https://www.google.com"
filter:
  nodes_per_region: 10
  deduplicate: true
`, filepath.Join(dir, "data.db"))
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func runInitForTest(t *testing.T, configPath, password string) error {
	t.Helper()
	return runInit([]string{
		"--config", configPath,
		"--domain", "proxy.example.com",
		"--username", "ph1a2b",
		"--site-path", testSitePath,
		"--password-stdin",
	}, strings.NewReader(password), io.Discard)
}

func TestRunInit_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	configPath := writeInitConfig(t, dir)

	if err := runInitForTest(t, configPath, "correct horse battery\n"); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	st, err := store.Open(filepath.Join(dir, "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	initialized, err := st.IsSystemInitialized()
	if err != nil || !initialized {
		t.Errorf("IsSystemInitialized = %v, %v; want true, nil", initialized, err)
	}

	user, err := st.GetUserByUsername("ph1a2b")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if user.Role != store.RoleSuperAdmin {
		t.Errorf("role = %q, want %q", user.Role, store.RoleSuperAdmin)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte("correct horse battery")); err != nil {
		t.Errorf("stored hash does not match password: %v", err)
	}

	sp, err := st.GetSitePath()
	if err != nil || sp != testSitePath {
		t.Errorf("GetSitePath = %q, %v; want %q, nil", sp, err, testSitePath)
	}

	domain, err := st.GetSetting("domain")
	if err != nil || domain != "proxy.example.com" {
		t.Errorf("GetSetting(domain) = %q, %v; want proxy.example.com, nil", domain, err)
	}
}

func TestRunInit_SecondRunFails(t *testing.T) {
	dir := t.TempDir()
	configPath := writeInitConfig(t, dir)

	if err := runInitForTest(t, configPath, "correct horse battery\n"); err != nil {
		t.Fatalf("first runInit() error = %v", err)
	}
	err := runInitForTest(t, configPath, "another long password\n")
	if err == nil {
		t.Fatal("second runInit expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("error = %q, want it to mention 'already initialized'", err)
	}
}

func TestRunInit_RejectsBeforeTouchingDisk(t *testing.T) {
	dir := t.TempDir()
	configPath := writeInitConfig(t, dir)

	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{
			name:  "empty password",
			args:  []string{"--config", configPath, "--domain", "proxy.example.com", "--username", "ph1a2b", "--site-path", testSitePath, "--password-stdin"},
			stdin: "\n",
			want:  "empty password",
		},
		{
			name:  "short password",
			args:  []string{"--config", configPath, "--domain", "proxy.example.com", "--username", "ph1a2b", "--site-path", testSitePath, "--password-stdin"},
			stdin: "short\n",
			want:  "too short",
		},
		{
			name:  "invalid site path",
			args:  []string{"--config", configPath, "--domain", "proxy.example.com", "--username", "ph1a2b", "--site-path", "Short-1A", "--password-stdin"},
			stdin: "correct horse battery\n",
			want:  "--site-path",
		},
		{
			name:  "reserved username",
			args:  []string{"--config", configPath, "--domain", "proxy.example.com", "--username", "admin", "--site-path", testSitePath, "--password-stdin"},
			stdin: "correct horse battery\n",
			want:  "reserved",
		},
		{
			name:  "missing password-stdin flag",
			args:  []string{"--config", configPath, "--domain", "proxy.example.com", "--username", "ph1a2b", "--site-path", testSitePath},
			stdin: "correct horse battery\n",
			want:  "--password-stdin",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runInit(tc.args, strings.NewReader(tc.stdin), io.Discard)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "data.db")); !os.IsNotExist(statErr) {
				t.Errorf("database file should not exist after a rejected run (stat err = %v)", statErr)
			}
		})
	}
}

func TestRunInit_MissingConfig(t *testing.T) {
	err := runInit([]string{
		"--config", filepath.Join(t.TempDir(), "does-not-exist.yaml"),
		"--domain", "proxy.example.com",
		"--username", "ph1a2b",
		"--site-path", testSitePath,
		"--password-stdin",
	}, strings.NewReader("correct horse battery\n"), io.Discard)
	if err == nil {
		t.Fatal("missing config expected error, got nil")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Errorf("error = %q, want it to mention the config load failure", err)
	}
}

func TestRunSubcommand_DispatchInit(t *testing.T) {
	// init is dispatched (and fails on the missing flags before touching any
	// file), while unknown first arguments still fall through to service
	// startup.
	handled, err := runSubcommand("init", nil)
	if !handled {
		t.Error("init should be handled as a subcommand")
	}
	if err == nil {
		t.Error("init without flags should return an error")
	}
}
