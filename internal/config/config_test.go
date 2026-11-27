package config_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/youthesame/elport/internal/config"
)

type fakeKeyring struct {
	items map[string]string
	err   error
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.items[service+":"+user]
	if !ok {
		return "", config.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(service, user, password string) error {
	if f.err != nil {
		return f.err
	}
	if f.items == nil {
		f.items = map[string]string{}
	}
	f.items[service+":"+user] = password
	return nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	if f.err != nil {
		return f.err
	}
	k := service + ":" + user
	if _, ok := f.items[k]; !ok {
		return config.ErrNotFound
	}
	delete(f.items, k)
	return nil
}

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ELABFTW_BASE_URL", "")
	t.Setenv("ELABFTW_API_KEY", "")
	t.Setenv("ELPORT_READ_ONLY", "")
	return home
}

func TestKeyringNotFoundDoesNotFallbackToPlaintextTOML(t *testing.T) {

	home := setupHome(t)
	cfgDir := filepath.Join(home, ".config", "elport")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "default_profile = \"lab\"\n[profiles.lab]\nbase_url = \"https://elab.example.test\"\napi_key = \"plaintext-secret\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := config.Load(home, home, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	emptyKR := &fakeKeyring{items: map[string]string{}}
	if _, _, _, _, err := config.Resolve(data, "", "", emptyKR, nil); err == nil {
		t.Fatal("expected error when keyring returns ErrNotFound")
	}

	var warn bytes.Buffer
	brokenKR := &fakeKeyring{err: errors.New("dbus unavailable")}
	name, url, key, verify, err := config.Resolve(data, "", "", brokenKR, &warn)
	if err != nil {
		t.Fatalf("expected plaintext fallback when keyring backend is unavailable, got: %v", err)
	}
	if name != "lab" || url != "https://elab.example.test" || key.Reveal() != "plaintext-secret" || !verify {
		t.Fatalf("unexpected fallback result: name=%q url=%q key=%q verify=%v", name, url, key.Reveal(), verify)
	}
	if !strings.Contains(warn.String(), config.PlaintextWarning) {
		t.Errorf("expected plaintext warning, got %q", warn.String())
	}
}

func TestLoginLogoutPreservesUnknownTOMLKeys(t *testing.T) {

	_ = setupHome(t)
	cfgPath, err := config.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := "custom_top = \"keep-me\"\n[profiles.lab]\nbase_url = \"https://old.example.test\"\ncustom_profile = 123\n"
	if err := os.WriteFile(cfgPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	kr := &fakeKeyring{}
	if err := config.Login("lab", "https://new.example.test/", config.NewAPIKey("secret-1"), kr, nil); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := config.SetDefault("lab"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	var after map[string]any
	if _, err := toml.DecodeFile(cfgPath, &after); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if after["custom_top"] != "keep-me" {
		t.Errorf("custom_top lost: %#v", after)
	}
	profs := after["profiles"].(map[string]any)
	lab := profs["lab"].(map[string]any)
	if lab["custom_profile"] != int64(123) || lab["base_url"] != "https://new.example.test" {
		t.Errorf("profile mismatch: %#v", lab)
	}

	removed, err := config.Logout("lab", kr)
	if err != nil || !removed {
		t.Fatalf("Logout = (%v, %v), want (true, nil)", removed, err)
	}
}

func TestLoadProtectsUserProfileBaseURLFromProjectOverride(t *testing.T) {

	_ = setupHome(t)
	cfgPath, _ := config.ConfigPath()
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)
	_ = os.WriteFile(cfgPath, []byte("default_profile = \"lab\"\n[profiles.lab]\nbase_url = \"https://real.example.test\"\nverify_ssl = true\n"), 0o600)

	proj := t.TempDir()
	docDir := filepath.Join(proj, "exp1")
	_ = os.MkdirAll(docDir, 0o755)
	_ = os.WriteFile(filepath.Join(proj, ".elport.toml"), []byte("[profiles.lab]\nbase_url = \"https://evil.example.test\"\nverify_ssl = false\nignore = [\"*.tmp\"]\n"), 0o644)
	_ = os.WriteFile(filepath.Join(proj, ".elportignore"), []byte("# comment\n*.bak\n"), 0o644)

	var warn bytes.Buffer
	data, err := config.Load(proj, docDir, &warn)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, baseURL, verify, err := config.BaseTarget(data, "", "")
	if err != nil {
		t.Fatalf("BaseTarget: %v", err)
	}
	if baseURL != "https://real.example.test" || !verify {
		t.Fatalf("user profile overridden: url=%q verify=%v", baseURL, verify)
	}
	if !strings.Contains(warn.String(), "ignoring") {
		t.Errorf("expected override warning, got %q", warn.String())
	}
	if len(data.IgnoreLayers) == 0 {
		t.Error("expected .elportignore layer to be loaded")
	}
}

func TestConfigLoginEnforces0600EvenOver0644File(t *testing.T) {

	_ = setupHome(t)
	cfgPath, _ := config.ConfigPath()
	_ = os.MkdirAll(filepath.Dir(cfgPath), 0o700)
	_ = os.WriteFile(cfgPath, []byte("default_profile = \"lab\"\n"), 0o644)

	brokenKR := &fakeKeyring{err: errors.New("no keyring")}
	var warn bytes.Buffer
	if err := config.Login("lab", "https://elab.example.test", config.NewAPIKey("secret"), brokenKR, &warn); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config.toml perm = %o, want 0600", info.Mode().Perm())
		}
	}
}

func TestResolveRefusesProjectOnlyProfileWithKeyringSecret(t *testing.T) {

	_ = setupHome(t)
	proj := t.TempDir()
	_ = os.WriteFile(filepath.Join(proj, ".elport.toml"), []byte("[profiles.lab]\nbase_url = \"https://evil.example.test\"\n"), 0o644)

	data, err := config.Load(proj, proj, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	kr := &fakeKeyring{items: map[string]string{"elport:lab": "my-secret"}}
	if _, _, _, _, err := config.Resolve(data, "lab", "", kr, nil); err == nil {
		t.Fatal("expected Resolve to refuse project-only profile with keyring secret")
	}
}

func TestLoadFailsOnUnreadableElportignoreDirectory(t *testing.T) {

	_ = setupHome(t)
	proj := t.TempDir()
	if err := os.Mkdir(filepath.Join(proj, ".elportignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(proj, proj, nil); err == nil {
		t.Fatal("expected error when .elportignore cannot be read as a file")
	}
}
