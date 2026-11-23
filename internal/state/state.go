package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/youthesame/elport/internal/fsx"
)

func Dir(create bool) (string, error) {
	cfgDir, err := fsx.ConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfgDir, "state")
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dir, 0o700); err != nil {
				return "", err
			}
		}
	}
	return dir, nil
}

func Key(base, entity, eid string) string {
	raw := strings.TrimRight(base, "/") + "|" + entity + "|" + eid
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func Path(base, entity, eid string) (string, error) {
	dir, err := Dir(false)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Key(base, entity, eid)), nil
}

func Load(base, entity, eid string) (map[string]any, error) {
	p, err := Path(base, entity, eid)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, nil
	}
	return data, nil
}

func Save(base, entity, eid string, data map[string]any) error {
	dir, err := Dir(true)
	if err != nil {
		return err
	}
	p := filepath.Join(dir, Key(base, entity, eid))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return err
	}
	out := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return fsx.AtomicWriteFile(p, out, 0o600)
}
