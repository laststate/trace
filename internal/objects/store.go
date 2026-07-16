package objects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is FS by default; optional S3-compatible (MinIO/R2) when Endpoint is set.
type Store struct {
	Root string

	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string

	mu     sync.Mutex
	client *http.Client
	cache  string
}

func (s *Store) s3Enabled() bool {
	return strings.TrimSpace(s.Endpoint) != "" && s.Bucket != ""
}

func (s *Store) Ensure() error {
	if s.Root == "" {
		return fmt.Errorf("object dir empty")
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	s.cache = filepath.Join(s.Root, ".cache")
	return os.MkdirAll(s.cache, 0o755)
}

func (s *Store) Put(raw []byte) (key, hash string, err error) {
	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:])
	key = filepath.ToSlash(filepath.Join(hash[:2], hash[2:4], hash))
	if s.s3Enabled() {
		return key, hash, s.s3Put(key, raw)
	}
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

func (s *Store) Get(key string) ([]byte, error) {
	if s.s3Enabled() {
		return s.s3Get(key)
	}
	return os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(key)))
}

func (s *Store) Path(key string) string {
	if !s.s3Enabled() {
		return filepath.Join(s.Root, filepath.FromSlash(key))
	}
	cache := s.cache
	if cache == "" {
		cache = filepath.Join(s.Root, ".cache")
	}
	local := filepath.Join(cache, filepath.FromSlash(key))
	if _, err := os.Stat(local); err == nil {
		return local
	}
	raw, err := s.s3Get(key)
	if err != nil {
		return local
	}
	_ = os.MkdirAll(filepath.Dir(local), 0o755)
	_ = os.WriteFile(local, raw, 0o644)
	return local
}

func (s *Store) httpClient() *http.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		s.client = &http.Client{Timeout: 60 * time.Second}
	}
	return s.client
}

func (s *Store) objectURL(key string) (string, error) {
	base := strings.TrimRight(s.Endpoint, "/")
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.Bucket + "/" + strings.TrimPrefix(key, "/")
	return u.String(), nil
}

func (s *Store) s3Put(key string, raw []byte) error {
	u, err := s.objectURL(key)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(raw))
	if err := s.sign(req, raw); err != nil {
		return err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("s3 put %s: %s", resp.Status, string(b))
	}
	return nil
}

func (s *Store) s3Get(key string) ([]byte, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if err := s.sign(req, nil); err != nil {
		return nil, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("s3 get %s: %s", resp.Status, string(b))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

func (s *Store) sign(req *http.Request, body []byte) error {
	if s.AccessKey == "" || s.SecretKey == "" {
		return nil
	}
	return signAWS4(req, body, s.AccessKey, s.SecretKey, s.Region, s.Bucket)
}
