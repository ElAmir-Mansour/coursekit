package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ElAmir-Mansour/coursekit/internal/model"
)

// Options controls a scan.
type Options struct {
	// Root is the course directory to read.
	Root string

	// Full requests complete ffprobe metadata for every file instead of the
	// cheap in-process duration read. Required before linting codecs or audio.
	Full bool

	// MaxDepth limits how deep chapter folders may nest, counted from the
	// root. Zero means no limit.
	MaxDepth int

	// Workers is the probe concurrency. Zero picks a sensible default.
	Workers int

	// Cache remembers probe results between runs. Nil disables caching.
	Cache *Cache

	// Progress, when set, is called as files are probed. It is invoked from
	// worker goroutines and must be safe to call concurrently.
	Progress func(Progress)
}

// Progress describes how far along a scan is.
type Progress struct {
	Phase   string
	Done    int
	Total   int
	Current string
}

// Result carries a scanned course plus a note of how the work was done, which
// the CLI reports so the speed difference is visible rather than magic.
type Result struct {
	Course    *model.Course
	Took      time.Duration
	CacheHits int
	FastPath  int
	FFprobed  int
}

// Scan reads a course directory and fills in media metadata.
//
// Files are grouped by the directory that holds them, so every folder
// containing recordings becomes a chapter. Chapters are then ordered by their
// parsed number rather than lexically, which is the whole reason a typo like
// "Chpater 3" does not end up sorted after "Chapter 9".
func Scan(ctx context.Context, opts Options) (*Result, error) {
	start := time.Now()

	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, &os.PathError{Op: "scan", Path: root, Err: os.ErrInvalid}
	}

	byDir, err := collect(root, opts.MaxDepth)
	if err != nil {
		return nil, err
	}

	course := &model.Course{
		Root:  root,
		Title: filepath.Base(root),
	}

	var toProbe []*model.MediaFile
	seen := map[string]bool{}

	for dir, files := range byDir {
		ch := &model.Chapter{
			Dir:    dir,
			IsRoot: dir == root,
		}

		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil || rel == "." {
			rel = ""
		}
		if ch.IsRoot {
			ch.Display = "(course root)"
			ch.Name = model.ChapterName{Order: model.Unnumbered}
		} else {
			// Order comes from the folder's own name; the display keeps the
			// full relative path so nested layouts stay readable.
			ch.Name = model.ParseChapter(filepath.Base(dir))
			ch.Display = rel
		}

		for _, f := range files {
			seen[f.Path] = true
			if f.Kind.Timed() {
				f.Lesson = model.ParseLesson(f.Name)
				ch.Lessons = append(ch.Lessons, f)
				toProbe = append(toProbe, f)
			} else {
				ch.Attachments = append(ch.Attachments, f)
			}
		}

		if len(ch.Lessons) == 0 && len(ch.Attachments) == 0 {
			continue
		}
		course.Chapters = append(course.Chapters, ch)
	}

	res := &Result{Course: course}
	if err := probeAll(ctx, toProbe, opts, res); err != nil {
		return nil, err
	}

	course.SortChapters()
	course.SortLessons()

	if opts.Cache != nil {
		opts.Cache.Prune(seen)
		// A cache that cannot be written is not worth failing a scan over.
		_ = opts.Cache.Save()
	}

	res.Took = time.Since(start)
	course.ScanTook = res.Took.Round(time.Millisecond).String()
	return res, nil
}

// collect walks the tree and groups media files by their containing directory.
func collect(root string, maxDepth int) (map[string][]*model.MediaFile, error) {
	byDir := map[string][]*model.MediaFile{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory should not abort the whole scan.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if path == root {
				return nil
			}
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			if maxDepth > 0 && depthFrom(root, path) > maxDepth {
				return fs.SkipDir
			}
			return nil
		}

		if skipFile(d.Name()) {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		// Zero-byte files are aborted recordings and interrupted copies, never
		// content. Counting them would misreport the course.
		if info.Size() == 0 {
			return nil
		}

		dir := filepath.Dir(path)
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}

		byDir[dir] = append(byDir[dir], &model.MediaFile{
			Path:    path,
			Rel:     rel,
			Name:    d.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Kind:    model.KindFor(path),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return byDir, nil
}

// probeAll fills in metadata for every timed file, using a worker pool.
//
// The fast path needs no subprocess, so concurrency here is about overlapping
// I/O; ffprobe genuinely benefits from one worker per core.
func probeAll(ctx context.Context, files []*model.MediaFile, opts Options, res *Result) error {
	if len(files) == 0 {
		return nil
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(files) {
		workers = len(files)
	}

	var (
		done      atomic.Int64
		cacheHits atomic.Int64
		fastPath  atomic.Int64
		ffprobed  atomic.Int64
	)
	total := len(files)

	jobs := make(chan *model.MediaFile)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				source := probeOne(ctx, f, opts)
				switch source {
				case sourceCache:
					cacheHits.Add(1)
				case sourceFast:
					fastPath.Add(1)
				case sourceFFprobe:
					ffprobed.Add(1)
				}

				n := int(done.Add(1))
				if opts.Progress != nil {
					opts.Progress(Progress{
						Phase:   "scan",
						Done:    n,
						Total:   total,
						Current: f.Name,
					})
				}
			}
		}()
	}

	for _, f := range files {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- f:
		}
	}
	close(jobs)
	wg.Wait()

	res.CacheHits = int(cacheHits.Load())
	res.FastPath = int(fastPath.Load())
	res.FFprobed = int(ffprobed.Load())
	return ctx.Err()
}

type probeSource int

const (
	sourceNone probeSource = iota
	sourceCache
	sourceFast
	sourceFFprobe
)

// probeOne resolves metadata for a single file, preferring the cache, then the
// in-process reader, then ffprobe.
func probeOne(ctx context.Context, f *model.MediaFile, opts Options) probeSource {
	fi, err := os.Stat(f.Path)
	if err != nil {
		f.ProbeErr = err.Error()
		return sourceNone
	}

	if info, ok := opts.Cache.Get(f.Path, fi, opts.Full); ok {
		f.Info = info
		return sourceCache
	}

	// The in-process reader covers MP4 and MOV, which is nearly every screen
	// recording, and costs about 0.2ms per file against ffprobe's ~60ms.
	if !opts.Full && model.FastPathEligible(f.Path) {
		if info, err := FastProbe(f.Path); err == nil {
			f.Info = info
			opts.Cache.Put(f.Path, fi, info)
			return sourceFast
		}
		// A malformed or unusual MP4 falls through to ffprobe rather than
		// being reported as broken.
	}

	info, err := FullProbe(ctx, f.Path)
	if err != nil {
		f.ProbeErr = err.Error()
		return sourceNone
	}
	f.Info = info
	opts.Cache.Put(f.Path, fi, info)
	return sourceFFprobe
}

func depthFrom(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// skipDir hides directories that never hold course content.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "__macosx", "node_modules", "$recycle.bin", "system volume information":
		return true
	}
	// Editor scratch folders, which are full of proxy media that would be
	// counted as lessons.
	switch name {
	case "Adobe Premiere Pro Auto-Save", "Motion Graphics Template Media",
		"Adobe Premiere Pro Audio Previews", "Adobe Premiere Pro Video Previews",
		"Final Cut Backups", "Render Files", "Transcoded Media", "Proxy Media":
		return true
	}
	return false
}

// skipFile hides filesystem bookkeeping that is not course content.
func skipFile(name string) bool {
	if strings.HasPrefix(name, ".") {
		// Covers .DS_Store and AppleDouble sidecars like ._clip.mov, which
		// otherwise appear as tiny broken duplicates of every file.
		return true
	}
	switch strings.ToLower(name) {
	case "desktop.ini", "thumbs.db", "icon\r":
		return true
	}
	return false
}
