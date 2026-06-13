package x

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

// Cache is a tiny content-addressed disk cache for GET responses. Keys are a
// hash of the canonical request; entries expire by mtime against a per-call TTL.
type Cache struct {
	dir     string
	enabled bool
}

// NewCache returns a cache rooted at dir. When enabled is false every operation
// is a no-op so callers need not special-case --no-cache.
func NewCache(dir string, enabled bool) *Cache {
	return &Cache{dir: dir, enabled: enabled}
}

func (c *Cache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, h[:2], h[2:])
}

// Get returns the cached bytes for key if present and younger than ttl.
func (c *Cache) Get(key string, ttl time.Duration) ([]byte, bool) {
	if !c.enabled || ttl <= 0 {
		return nil, false
	}
	p := c.path(key)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, false
	}
	if time.Since(fi.ModTime()) > ttl {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Put writes bytes for key.
func (c *Cache) Put(key string, data []byte) {
	if !c.enabled {
		return
	}
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// Clear removes the whole cache directory.
func (c *Cache) Clear() error { return os.RemoveAll(c.dir) }

// Size returns the total bytes and file count of the cache.
func (c *Cache) Size() (bytes int64, files int) {
	_ = filepath.Walk(c.dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			bytes += fi.Size()
			files++
		}
		return nil
	})
	return bytes, files
}
