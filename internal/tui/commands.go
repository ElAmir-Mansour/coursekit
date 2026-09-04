package tui

import (
	"context"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/rename"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// Messages carried back into Update once background work finishes. Every slow
// operation goes through one of these rather than blocking Update, which would
// freeze the interface.
type (
	progressMsg scan.Progress
	scanDoneMsg struct {
		result *scan.Result
		err    error
	}
	doctorDoneMsg struct {
		report *lint.Report
		err    error
	}
	loudnessDoneMsg struct{ err error }
	planDoneMsg     struct {
		plan     *rename.Plan
		problems []rename.Problem
		err      error
	}
	appliedMsg struct {
		journal *rename.Journal
		err     error
	}
	undoneMsg struct {
		count int
		err   error
	}
	exportedMsg struct {
		path string
		err  error
	}
)

// progressStream carries progress ticks from worker goroutines into the
// update loop.
//
// Pushes are non-blocking and drop when the buffer is full: progress is
// cosmetic, and a full channel must never be allowed to stall the scan that
// is producing it.
type progressStream struct {
	ch chan scan.Progress
}

func newProgressStream() *progressStream {
	return &progressStream{ch: make(chan scan.Progress, 64)}
}

func (p *progressStream) push(pr scan.Progress) {
	select {
	case p.ch <- pr:
	default:
	}
}

// waitProgress blocks on the next tick. Update re-issues it after each one,
// which is the standard way to consume a channel in Bubble Tea.
func waitProgress(p *progressStream) tea.Cmd {
	return func() tea.Msg {
		pr, ok := <-p.ch
		if !ok {
			return nil
		}
		return progressMsg(pr)
	}
}

func cacheFor(cfg Config) *scan.Cache {
	if cfg.NoCache {
		return scan.NoCache()
	}
	return scan.OpenCache(cfg.Root)
}

// startScan kicks off a scan and returns commands for both its progress and
// its result.
func startScan(ctx context.Context, cfg Config, full bool) tea.Cmd {
	stream := newProgressStream()
	done := make(chan scanDoneMsg, 1)

	go func() {
		res, err := scan.Scan(ctx, scan.Options{
			Root:     cfg.Root,
			Full:     full,
			Workers:  cfg.Workers,
			Cache:    cacheFor(cfg),
			Progress: stream.push,
		})
		done <- scanDoneMsg{result: res, err: err}
		close(stream.ch)
	}()

	return tea.Batch(
		waitProgress(stream),
		func() tea.Msg { return <-done },
		func() tea.Msg { return attachStreamMsg{stream} },
	)
}

// attachStreamMsg hands the stream to the model so it can keep asking for
// more ticks.
type attachStreamMsg struct{ stream *progressStream }

// fullScanThenDoctor re-reads the course with ffprobe and then checks it.
//
// doctor cannot run on a fast scan: codec, bitrate and audio detail are simply
// not there, and guessing at them would produce confident nonsense.
func fullScanThenDoctor(ctx context.Context, cfg Config, profileName string) tea.Cmd {
	return func() tea.Msg {
		if err := scan.FFprobeAvailable(); err != nil {
			return doctorDoneMsg{err: err}
		}

		res, err := scan.Scan(ctx, scan.Options{
			Root:    cfg.Root,
			Full:    true,
			Workers: cfg.Workers,
			Cache:   cacheFor(cfg),
		})
		if err != nil {
			return doctorDoneMsg{err: err}
		}

		profile, err := lint.LoadProfile(profileName)
		if err != nil {
			return doctorDoneMsg{err: err}
		}
		return doctorDoneMsg{report: lint.Check(res.Course, profile)}
	}
}

// runDoctor re-checks an already-probed course, for switching profiles.
func runDoctor(course *model.Course, profileName string) tea.Cmd {
	return func() tea.Msg {
		if course == nil {
			return doctorDoneMsg{err: nil}
		}
		profile, err := lint.LoadProfile(profileName)
		if err != nil {
			return doctorDoneMsg{err: err}
		}
		return doctorDoneMsg{report: lint.Check(course, profile)}
	}
}

func measureLoudness(ctx context.Context, cfg Config, course *model.Course) tea.Cmd {
	return func() tea.Msg {
		err := scan.MeasureCourseLoudness(ctx, course, cacheFor(cfg), nil)
		return loudnessDoneMsg{err: err}
	}
}

func buildPlan(course *model.Course, cfg Config) tea.Cmd {
	return func() tea.Msg {
		plan, err := rename.Build(course, rename.Options{
			ChapterTemplate: rename.DefaultChapterTemplate,
			StripPrefix:     cfg.StripPrefix,
		})
		if err != nil {
			return planDoneMsg{err: err}
		}
		return planDoneMsg{plan: plan, problems: rename.Validate(plan)}
	}
}

func applyPlan(plan *rename.Plan) tea.Cmd {
	return func() tea.Msg {
		journal, err := rename.Apply(plan)
		return appliedMsg{journal: journal, err: err}
	}
}

func undoLast(root string) tea.Cmd {
	return func() tea.Msg {
		journal, err := rename.LatestJournal(root)
		if err != nil {
			return undoneMsg{err: err}
		}
		n, err := rename.Undo(journal)
		return undoneMsg{count: n, err: err}
	}
}

func exportMarkdown(course *model.Course) tea.Cmd {
	return func() tea.Msg {
		path := defaultExportName(course)
		f, err := os.Create(path)
		if err != nil {
			return exportedMsg{err: err}
		}
		if err := export.Markdown(f, course); err != nil {
			_ = f.Close()
			return exportedMsg{err: err}
		}
		// A close error on a report matters: it can mean a short write, and
		// reporting success for a truncated file would be a lie.
		if err := f.Close(); err != nil {
			return exportedMsg{err: err}
		}
		return exportedMsg{path: path}
	}
}
