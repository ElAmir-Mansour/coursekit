package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// cacheVersion is bumped whenever the stored shape changes, so an old file is
// discarded rather than misread.
const cacheVersion = 1

// Entry is one file's remembered metadata, along with the stat fields used to
// notice that the file has changed underneath us.
type Entry struct {
	Size     int64           `json:"size"`
	ModUnixN int64           `json:"mod_unix_nano"`
	Info     model.MediaInfo `json:"info"`
}

type cacheFile struct {
	Version int              `json:"version"`
	Root    string           `json:"root"`
	Entries map[string]Entry `json:"entries"`
}

// Cache remembers probe results between runs, keyed on path plus size plus
// modification time so an edited file is never served stale.
//
// It lives in the user's cache directory rather than inside the course folder:
// scanning is something people expect to be read-only, and a tool that
// scatters state through someone's recordings is a tool they stop trusting.
// Loudness measurements matter most here, at seconds per file to recompute.
type Cache struct {
	path string

	mu      sync.Mutex
	entries map[string]Entry
	dirty   bool
	enabled bool
}

// CacheDir returns the directory coursekit keeps its state in, honouring
// XDG_CACHE_HOME where it is set.
func CacheDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "coursekit"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "coursekit"), nil
}

// CacheKeyForRoot derives a stable filename for a course root. The hash keeps
// the name short and filesystem-safe while the prefix keeps it recognisable
// when someone goes looking through the cache directory by hand.
func CacheKeyForRoot(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	return sanitizeForFilename(filepath.Base(abs)) + "-" + hex.EncodeToString(sum[:6])
}

// OpenCache loads the cache for a course root. A corrupt or outdated file is
// treated as an empty cache, never as a fatal error: a cache is an
// optimisation, and losing it should only ever cost time.
func OpenCache(root string) *Cache {
	dir, err := CacheDir()
	if err != nil {
		return &Cache{entries: map[string]Entry{}}
	}
	path := filepath.Join(dir, CacheKeyForRoot(root)+".json")

	c := &Cache{path: path, entries: map[string]Entry{}, enabled: true}

	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil || cf.Version != cacheVersion {
		return c
	}
	if cf.Entries != nil {
		c.entries = cf.Entries
	}
	return c
}

// NoCache returns a cache that remembers nothing, for --no-cache runs.
func NoCache() *Cache { return &Cache{entries: map[string]Entry{}} }

// Path is where the cache is stored on disk, empty when caching is off.
func (c *Cache) Path() string {
	if c == nil || !c.enabled {
		return ""
	}
	return c.path
}

// Get returns remembered metadata for a file, if the file still matches the
// size and modification time it had when the entry was written.
//
// wantFull demands a record produced by ffprobe; a cheap fast-path record will
// not satisfy it, because codec and audio fields would be missing. The reverse
// is fine, so a full record always answers a fast query.
func (c *Cache) Get(path string, fi os.FileInfo, wantFull bool) (model.MediaInfo, bool) {
	if c == nil {
		return model.MediaInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[path]
	if !ok {
		return model.MediaInfo{}, false
	}
	if e.Size != fi.Size() || e.ModUnixN != fi.ModTime().UnixNano() {
		return model.MediaInfo{}, false
	}
	if wantFull && !e.Info.Full {
		return model.MediaInfo{}, false
	}
	return e.Info, true
}

// GetLoudness returns a remembered loudness measurement for an unchanged file.
func (c *Cache) GetLoudness(path string, fi os.FileInfo) (*model.Loudness, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[path]
	if !ok || e.Info.Loudness == nil {
		return nil, false
	}
	if e.Size != fi.Size() || e.ModUnixN != fi.ModTime().UnixNano() {
		return nil, false
	}
	return e.Info.Loudness, true
}

// Put stores metadata for a file, preserving any loudness measurement already
// remembered for it so a plain rescan does not throw away expensive work.
func (c *Cache) Put(path string, fi os.FileInfo, info model.MediaInfo) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if prev, ok := c.entries[path]; ok && info.Loudness == nil {
		if prev.Size == fi.Size() && prev.ModUnixN == fi.ModTime().UnixNano() {
			info.Loudness = prev.Info.Loudness
		}
	}

	c.entries[path] = Entry{
		Size:     fi.Size(),
		ModUnixN: fi.ModTime().UnixNano(),
		Info:     info,
	}
	c.dirty = true
}

// Prune drops entries for files that were not seen in this scan, so the cache
// does not grow without bound as a course is reorganised.
func (c *Cache) Prune(seen map[string]bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	for k := range c.entries {
		if !seen[k] {
			delete(c.entries, k)
			c.dirty = true
		}
	}
}

// Save writes the cache back to disk if anything changed. The write goes to a
// temporary file first and is then renamed, so an interrupted save cannot
// leave a half-written cache behind.
func (c *Cache) Save() error {
	if c == nil || !c.enabled {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.dirty {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}

	data, err := json.Marshal(cacheFile{
		Version: cacheVersion,
		Entries: c.entries,
	})
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best effort: a leftover temp file costs nothing and the rename below
	// is what matters.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		return err
	}

	c.dirty = false
	return nil
}

// sanitizeForFilename reduces a course folder name to something safe to embed
// in a cache filename on any filesystem.
func sanitizeForFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	if len(out) == 0 {
		return "course"
	}
	return string(out)
}
