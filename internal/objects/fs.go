package objects

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	Root string
}

func (s Store) Put(raw []byte) (key, hash string, err error) {
	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:])
	key = filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], hash))
	full := filepath.Join(s.Root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", "", err
	}
	if _, err := os.Stat(full); err == nil {
		return key, hash, nil
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	return key, hash, nil
}

func (s Store) Get(key string) ([]byte, error) {
	full := filepath.Join(s.Root, filepath.FromSlash(key))
	return os.ReadFile(full)
}

func (s Store) Path(key string) string {
	return filepath.Join(s.Root, filepath.FromSlash(key))
}

func (s Store) Ensure() error {
	if s.Root == "" {
		return fmt.Errorf("object dir empty")
	}
	return os.MkdirAll(s.Root, 0o755)
}
