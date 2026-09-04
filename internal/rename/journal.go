package rename

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// journalVersion guards the on-disk format.
const journalVersion = 1

// Journal is the record of one applied rename, written so it can be replayed
// backwards. Reconstructing an undo by re-deriving names would depend on the
// templates and the parser behaving identically later; recording the exact
// moves does not.
type Journal struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Root    string    `json:"root"`
	Ops     []Op      `json:"ops"`

	// Pending marks a journal whose apply never reported completion, which
	// means the process was killed part-way through.
	Pending bool `json:"pending,omitempty"`

	// Undone marks a journal that has already been reversed, so a second
	// undo does not try to reverse it again.
	Undone bool `json:"undone,omitempty"`

	path string
}

// Path is where the journal is stored.
func (j *Journal) Path() string { return j.path }

// JournalDir is where journals for a course root are kept. Like the metadata
// cache, this lives under the user's state directory rather than inside the
// course, so coursekit never leaves anything behind in someone's recordings.
func JournalDir(root string) (string, error) {
	base, err := scan.CacheDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return filepath.Join(base, "journal", scan.CacheKeyForRoot(abs)), nil
}

// newJournal creates a journal and writes it in the pending state, before any
// file is touched. If the process dies mid-apply, this file is what makes the
// damage discoverable instead of invisible.
func newJournal(p *Plan) (*Journal, error) {
	dir, err := JournalDir(p.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	now := time.Now()
	j := &Journal{
		Version: journalVersion,
		Created: now,
		Root:    p.Root,
		Ops:     append([]Op(nil), p.Ops...),
		Pending: true,
		path:    filepath.Join(dir, now.UTC().Format("20060102-150405")+".json"),
	}
	if err := j.write(); err != nil {
		return nil, err
	}
	return j, nil
}

// commit records that every op in the journal was applied successfully.
func (j *Journal) commit(applied []Op) error {
	j.Ops = applied
	j.Pending = false
	return j.write()
}

// markUndone records that this journal has been reversed.
func (j *Journal) markUndone() error {
	j.Undone = true
	return j.write()
}

func (j *Journal) write() error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.path, data, 0o644)
}

// discard removes a journal, used when an apply was fully rolled back and
// therefore left nothing behind to undo.
func (j *Journal) discard() {
	if j.path != "" {
		_ = os.Remove(j.path)
	}
}

// LoadJournal reads a journal file.
func LoadJournal(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("parse journal %s: %w", path, err)
	}
	if j.Version != journalVersion {
		return nil, fmt.Errorf("journal %s was written by a different version (%d)", path, j.Version)
	}
	j.path = path
	return &j, nil
}

// ListJournals returns every journal for a course root, newest first.
func ListJournals(root string) ([]*Journal, error) {
	dir, err := JournalDir(root)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []*Journal
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		j, err := LoadJournal(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, j)
	}

	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out, nil
}

// LatestJournal returns the most recent journal for a course root.
func LatestJournal(root string) (*Journal, error) {
	all, err := ListJournals(root)
	if err != nil {
		return nil, err
	}
	for _, j := range all {
		if !j.Undone {
			return j, nil
		}
	}
	if len(all) > 0 {
		return nil, fmt.Errorf("the last rename of %s has already been undone", root)
	}
	return nil, fmt.Errorf("no rename to undo for %s", root)
}
