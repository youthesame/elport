package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/zalando/go-keyring"

	"github.com/youthesame/elport/internal/fsx"
)

const PlaintextWarning = "note: no OS keyring; api_key stored in ~/.config/elport/config.toml (mode 600)"

const keyringService = "elport"

var ErrNotFound = keyring.ErrNotFound

type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type OSKeyring struct{}

func (OSKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (OSKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (OSKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type IgnoreLayer struct {
	Patterns []string
	Prefix   string
}

type Data struct {
	Raw          map[string]any
	IgnoreLayers []IgnoreLayer
	UserProfiles map[string]bool
}

func ConfigPath() (string, error) {
	dir, err := fsx.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func RequireHTTPS(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return fmt.Errorf("base_url must start with https://: %s", baseURL)
	}
	return nil
}

func readTOML(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	data := map[string]any{}
	if _, err := toml.Decode(string(raw), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func writeTOML(path string, data map[string]any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(data); err != nil {
		return err
	}
	return fsx.AtomicWriteFileFixedPerm(path, buf.Bytes(), 0o600)
}

func cleanPatterns(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		s := strings.TrimSpace(p)
		if s != "" && !strings.HasPrefix(p, "#") {
			out = append(out, s)
		}
	}
	return out
}

func toStringSlice(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if res, err := filepath.EvalSymlinks(abs); err == nil {
		return res
	}
	return abs
}

func makeIgnoreLayer(base, docDir string, patterns []string) (IgnoreLayer, bool) {
	cleaned := cleanPatterns(patterns)
	if len(cleaned) == 0 {
		return IgnoreLayer{}, false
	}
	rel, err := filepath.Rel(resolvePath(base), resolvePath(docDir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return IgnoreLayer{}, false
	}
	prefix := filepath.ToSlash(rel)
	if prefix == "." {
		prefix = ""
	}
	return IgnoreLayer{Patterns: cleaned, Prefix: prefix}, true
}

func ignoreDirs(project, docDir string) []string {
	proj := resolvePath(project)
	doc := resolvePath(docDir)
	rel, err := filepath.Rel(proj, doc)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []string{doc}
	}
	chain := []string{doc}
	for chain[len(chain)-1] != proj {
		parent := filepath.Dir(chain[len(chain)-1])
		if parent == chain[len(chain)-1] {
			return []string{doc}
		}
		chain = append(chain, parent)
	}
	slices.Reverse(chain)
	return chain
}

func profilesMap(data map[string]any) map[string]any {
	raw, ok := data["profiles"]
	if !ok {
		return nil
	}
	m, _ := raw.(map[string]any)
	return m
}

func ensureProfilesMap(data map[string]any) map[string]any {
	if m := profilesMap(data); m != nil {
		return m
	}
	m := map[string]any{}
	data["profiles"] = m
	return m
}

func profileEntry(data map[string]any, name string) map[string]any {
	profs := profilesMap(data)
	if profs == nil {
		return nil
	}
	p, _ := profs[name].(map[string]any)
	return p
}

func Load(project, docDir string, warn io.Writer) (*Data, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := readTOML(cfgPath)
	if err != nil {
		return nil, err
	}
	userProfiles := map[string]bool{}
	for name := range profilesMap(data) {
		userProfiles[name] = true
	}

	var layers []IgnoreLayer
	addIgnore := func(base string, patterns []string) {
		if layer, ok := makeIgnoreLayer(base, docDir, patterns); ok {
			layers = append(layers, layer)
		}
	}

	if rawIgnore, exists := data["ignore"]; exists {
		addIgnore(docDir, toStringSlice(rawIgnore))
		delete(data, "ignore")
	}

	for _, dir := range ignoreDirs(project, docDir) {
		ignoreFile := filepath.Join(dir, ".elportignore")
		raw, err := os.ReadFile(ignoreFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
		addIgnore(dir, lines)
	}

	seen := map[string]bool{}
	for _, p := range []string{filepath.Join(project, ".elport.toml"), filepath.Join(docDir, ".elport.toml")} {
		res := resolvePath(p)
		if seen[res] {
			continue
		}
		seen[res] = true
		layer, err := readTOML(p)
		if err != nil {
			return nil, err
		}
		if len(layer) == 0 {
			continue
		}
		if layerProfs := profilesMap(layer); layerProfs != nil {
			profs := ensureProfilesMap(data)
			for name, rawVals := range layerProfs {
				vals, ok := rawVals.(map[string]any)
				if !ok {
					continue
				}
				prof, ok := profs[name].(map[string]any)
				if !ok {
					prof = map[string]any{}
					profs[name] = prof
				}
				for k, v := range vals {
					if userProfiles[name] && (k == "base_url" || k == "verify_ssl") {
						if warn != nil {
							_, _ = fmt.Fprintf(warn, "warning: ignoring %s override of profiles.%s.%s\n", p, name, k)
						}
						continue
					}
					prof[k] = v
				}
			}
		}
		for k, v := range layer {
			if k == "ignore" {
				addIgnore(filepath.Dir(p), toStringSlice(v))
			} else if k != "profiles" {
				data[k] = v
			}
		}
	}

	return &Data{Raw: data, IgnoreLayers: layers, UserProfiles: userProfiles}, nil
}

func ProfileName(data *Data, cliProfile, metaProfile string) (string, error) {
	if metaProfile != "" && cliProfile != "" && metaProfile != cliProfile {
		return "", errors.New("profile mismatch between frontmatter and CLI")
	}
	if metaProfile != "" {
		return metaProfile, nil
	}
	if cliProfile != "" {
		return cliProfile, nil
	}
	if data != nil {
		if def, ok := data.Raw["default_profile"].(string); ok && def != "" {
			return def, nil
		}
		profs := profilesMap(data.Raw)
		if len(profs) == 0 {
			return "default", nil
		}
		if _, hasDefault := profs["default"]; hasDefault {
			return "default", nil
		}
		return "", errors.New("no default profile set; run 'elport profile use <name>'")
	}
	return "default", nil
}

func boolDefaultTrue(m map[string]any, key string) bool {
	if m == nil {
		return true
	}
	v, ok := m[key].(bool)
	if !ok {
		return true
	}
	return v
}

func BaseTarget(data *Data, cliProfile, metaProfile string) (string, string, bool, error) {
	name, err := ProfileName(data, cliProfile, metaProfile)
	if err != nil {
		return "", "", false, err
	}
	var prof map[string]any
	if data != nil {
		prof = profileEntry(data.Raw, name)
	}
	baseURL := os.Getenv("ELABFTW_BASE_URL")
	if baseURL == "" && prof != nil {
		baseURL, _ = prof["base_url"].(string)
	}
	if baseURL == "" {
		return "", "", false, fmt.Errorf("base_url unavailable for profile %s; run elport login", name)
	}
	return name, strings.TrimRight(baseURL, "/"), boolDefaultTrue(prof, "verify_ssl"), nil
}

func Resolve(data *Data, cliProfile, metaProfile string, kr Keyring, warn io.Writer) (string, string, APIKey, bool, error) {
	name, err := ProfileName(data, cliProfile, metaProfile)
	if err != nil {
		return "", "", APIKey{}, false, err
	}
	envURL := os.Getenv("ELABFTW_BASE_URL")
	envKey := os.Getenv("ELABFTW_API_KEY")
	if (envURL != "") != (envKey != "") {
		return "", "", APIKey{}, false, errors.New("ELABFTW_BASE_URL and ELABFTW_API_KEY must be set together")
	}
	if envURL != "" && envKey != "" {
		return name, strings.TrimRight(envURL, "/"), NewAPIKey(envKey), true, nil
	}
	var prof map[string]any
	if data != nil {
		if data.UserProfiles != nil && !data.UserProfiles[name] {
			return "", "", APIKey{}, false, fmt.Errorf("profile %s is not configured in user config; run elport login", name)
		}
		prof = profileEntry(data.Raw, name)
	}
	baseURL, _ := prof["base_url"].(string)
	if kr == nil {
		kr = OSKeyring{}
	}
	secret, err := kr.Get(keyringService, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", "", APIKey{}, false, fmt.Errorf("credentials unavailable for profile %s; run elport login", name)
		}
		cfgPath, pathErr := ConfigPath()
		if pathErr != nil {
			return "", "", APIKey{}, false, pathErr
		}
		userData, readErr := readTOML(cfgPath)
		if readErr != nil {
			return "", "", APIKey{}, false, readErr
		}
		fb := profileEntry(userData, name)
		fbURL, _ := fb["base_url"].(string)
		fbKey, _ := fb["api_key"].(string)
		if fbURL == "" || fbKey == "" {
			return "", "", APIKey{}, false, errors.New("keyring unavailable; set ELABFTW_BASE_URL and ELABFTW_API_KEY together, or run elport login")
		}
		if warn != nil {
			_, _ = fmt.Fprintln(warn, PlaintextWarning)
		}
		return name, strings.TrimRight(fbURL, "/"), NewAPIKey(fbKey), boolDefaultTrue(fb, "verify_ssl"), nil
	}
	if baseURL == "" || secret == "" {
		return "", "", APIKey{}, false, fmt.Errorf("credentials unavailable for profile %s; run elport login", name)
	}
	return name, strings.TrimRight(baseURL, "/"), NewAPIKey(secret), boolDefaultTrue(prof, "verify_ssl"), nil
}

func Login(name, rawURL string, apiKey APIKey, kr Keyring, warn io.Writer) error {
	if err := RequireHTTPS(rawURL); err != nil {
		return err
	}
	cfgPath, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := readTOML(cfgPath)
	if err != nil {
		return err
	}
	profs := ensureProfilesMap(data)
	prof, ok := profs[name].(map[string]any)
	if !ok {
		prof = map[string]any{}
		profs[name] = prof
	}
	prof["base_url"] = strings.TrimRight(rawURL, "/")
	delete(prof, "api_key")
	if def, _ := data["default_profile"].(string); def == "" {
		data["default_profile"] = name
	}
	if err := writeTOML(cfgPath, data); err != nil {
		return err
	}
	if kr == nil {
		kr = OSKeyring{}
	}
	if err := kr.Set(keyringService, name, apiKey.Reveal()); err != nil {
		prof["api_key"] = apiKey.Reveal()
		if writeErr := writeTOML(cfgPath, data); writeErr != nil {
			return writeErr
		}
		if warn != nil {
			_, _ = fmt.Fprintln(warn, PlaintextWarning)
		}
	}
	return nil
}

func Logout(name string, kr Keyring) (bool, error) {
	removed := false
	if kr == nil {
		kr = OSKeyring{}
	}
	if err := kr.Delete(keyringService, name); err == nil {
		removed = true
	}
	cfgPath, err := ConfigPath()
	if err != nil {
		return removed, err
	}
	data, err := readTOML(cfgPath)
	if err != nil {
		return removed, err
	}
	changed := false
	if prof := profileEntry(data, name); prof != nil {
		if _, hasKey := prof["api_key"]; hasKey {
			delete(prof, "api_key")
			removed = true
			changed = true
		}
	}
	if def, _ := data["default_profile"].(string); def == name {
		delete(data, "default_profile")
		changed = true
	}
	if changed {
		if err := writeTOML(cfgPath, data); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func SetDefault(name string) error {
	cfgPath, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := readTOML(cfgPath)
	if err != nil {
		return err
	}
	data["default_profile"] = name
	return writeTOML(cfgPath, data)
}
