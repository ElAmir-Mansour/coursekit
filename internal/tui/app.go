// Package tui is coursekit's interactive course browser.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/rename"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// Config is what the CLI hands to the interface.
type Config struct {
	Root        string
	Profile     string
	Workers     int
	NoCache     bool
	StripPrefix bool
}

// screen identifies which panel is showing.
type screen int

const (
	screenTree screen = iota
	screenDoctor
	screenRename
	screenHelp
)

// sortMode orders the chapter list.
type sortMode int

const (
	sortSyllabus sortMode = iota
	sortDuration
	sortSize
	sortName
)

func (s sortMode) String() string {
	switch s {
	case sortDuration:
		return "duration"
	case sortSize:
		return "size"
	case sortName:
		return "name"
	default:
		return "syllabus"
	}
}

// Model is the interface state.
type Model struct {
	cfg  Config
	keys keyMap
	th   Theme

	width, height int

	spin      spinner.Model
	filter    textinput.Model
	filtering bool

	// data
	course *model.Course
	stats  export.ScanStats
	busy   string
	prog   scan.Progress
	err    error

	// tree state
	rows     []treeRow
	cursor   int
	offset   int
	expanded map[string]bool

	sort sortMode

	// doctor state
	report       *lint.Report
	profile      string
	loudnessDone bool

	// rename state
	plan       *rename.Plan
	problems   []rename.Problem
	journal    *rename.Journal
	confirming bool

	screen screen
	status statusLine

	stream *progressStream
	ctx    context.Context
}

type statusLine struct {
	text string
	kind statusKind
}

type statusKind int

const (
	statusInfo statusKind = iota
	statusGood
	statusBad
)

// profileCycle is the order the p key walks through.
var profileCycle = []string{"udemy", "youtube", "lms", "strict"}

// New builds the initial model.
func New(ctx context.Context, cfg Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textinput.New()
	ti.Placeholder = "filter by name"
	ti.Prompt = "/"
	ti.CharLimit = 80

	profile := cfg.Profile
	if profile == "" {
		profile = "udemy"
	}

	return Model{
		cfg: cfg,
		// Start at a conventional size rather than zero. Some terminals and
		// most pseudo-terminals report no size until asked, and a model that
		// refuses to draw until it is told its dimensions renders nothing at
		// all in those cases.
		width:    100,
		height:   30,
		keys:     newKeyMap(),
		th:       NewTheme(true),
		spin:     sp,
		filter:   ti,
		expanded: map[string]bool{},
		profile:  profile,
		ctx:      ctx,
	}
}

// Init starts the first scan and asks the terminal for its background colour.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		tea.RequestWindowSize,
		m.spin.Tick,
		startScan(m.ctx, m.cfg, false),
	)
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A zero size means the terminal could not tell us; keep what we have.
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.clampCursor()
		return m, nil

	case tea.BackgroundColorMsg:
		// Recolour once the real terminal background is known, so the theme
		// is right rather than merely assumed.
		m.th = NewTheme(msg.IsDark())
		return m, nil

	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case attachStreamMsg:
		m.stream = msg.stream
		return m, nil

	case progressMsg:
		m.prog = scan.Progress(msg)
		if m.stream != nil {
			return m, waitProgress(m.stream)
		}
		return m, nil

	case scanDoneMsg:
		return m.onScanDone(msg)

	case doctorDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, nil
		}
		m.report = msg.report
		m.screen = screenDoctor
		m.offset, m.cursor = 0, 0
		e, w, _ := msg.report.Counts()
		m.status = statusLine{
			fmt.Sprintf("%s: %d error(s), %d warning(s)", msg.report.Profile, e, w),
			statusKindFor(e, w),
		}
		return m, nil

	case loudnessDoneMsg:
		m.busy = ""
		m.loudnessDone = true
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, nil
		}
		m.status = statusLine{"loudness measured — press d to re-check", statusGood}
		// Re-run the check so the new measurements are reflected immediately.
		return m, runDoctor(m.course, m.profile)

	case planDoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, nil
		}
		m.plan, m.problems = msg.plan, msg.problems
		m.screen = screenRename
		m.confirming = false
		m.offset, m.cursor = 0, 0
		m.status = statusLine{"dry run — nothing changed yet", statusInfo}
		return m, nil

	case appliedMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, nil
		}
		m.journal = msg.journal
		m.status = statusLine{
			fmt.Sprintf("applied %d rename(s) — press u to undo", len(msg.journal.Ops)),
			statusGood,
		}
		m.plan, m.problems, m.confirming = nil, nil, false
		m.screen = screenTree
		return m, startScan(m.ctx, m.cfg, false)

	case undoneMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, startScan(m.ctx, m.cfg, false)
		}
		m.journal = nil
		m.status = statusLine{fmt.Sprintf("reversed %d rename(s)", msg.count), statusGood}
		return m, startScan(m.ctx, m.cfg, false)

	case exportedMsg:
		m.busy = ""
		if msg.err != nil {
			m.status = statusLine{msg.err.Error(), statusBad}
			return m, nil
		}
		m.status = statusLine{"wrote " + msg.path, statusGood}
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}

	return m, nil
}

func (m Model) onScanDone(msg scanDoneMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	m.stream = nil

	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}

	m.err = nil
	m.course = msg.result.Course
	m.stats = export.ScanStats{
		Took:      msg.result.Took.Round(1e6).String(),
		CacheHits: msg.result.CacheHits,
		FastPath:  msg.result.FastPath,
		FFprobed:  msg.result.FFprobed,
	}
	m.rebuildRows()

	if m.status.text == "" {
		m.status = statusLine{
			fmt.Sprintf("%d lessons · %s", m.course.LessonCount(),
				model.HumanDuration(m.course.Duration())),
			statusInfo,
		}
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The filter input owns the keyboard while it is open, apart from the
	// keys that close it.
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.rebuildRows()
			return m, nil
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildRows()
		return m, cmd
	}

	// A pending confirmation must be answered before anything else happens,
	// so a stray keypress cannot rename a course by accident.
	if m.confirming {
		switch msg.String() {
		case "y", "Y":
			m.confirming = false
			m.busy = "applying"
			return m, tea.Batch(m.spin.Tick, applyPlan(m.plan))
		default:
			m.confirming = false
			m.status = statusLine{"cancelled — nothing changed", statusInfo}
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		if m.screen == screenHelp {
			m.screen = screenTree
		} else {
			m.screen = screenHelp
		}
		return m, nil

	case key.Matches(msg, m.keys.Back):
		if m.screen != screenTree {
			m.screen = screenTree
			m.offset, m.cursor = 0, 0
			m.clampCursor()
		}
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m.cursor--
		m.clampCursor()
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.cursor++
		m.clampCursor()
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		m.cursor -= m.bodyHeight()
		m.clampCursor()
		return m, nil

	case key.Matches(msg, m.keys.PageDown):
		m.cursor += m.bodyHeight()
		m.clampCursor()
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.cursor, m.offset = 0, 0
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.cursor = m.contentLines() - 1
		m.clampCursor()
		return m, nil
	}

	if m.busy != "" {
		// Long operations are already running; ignore anything that would
		// start another.
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Toggle):
		return m.toggleExpand(), nil

	case key.Matches(msg, m.keys.ExpandAll):
		return m.expandAll(), nil

	case key.Matches(msg, m.keys.Sort):
		m.sort = (m.sort + 1) % 4
		m.rebuildRows()
		m.status = statusLine{"sorted by " + m.sort.String(), statusInfo}
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filter.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Profile):
		m.profile = nextProfile(m.profile)
		m.status = statusLine{"profile: " + m.profile, statusInfo}
		if m.report != nil {
			m.busy = "checking"
			return m, tea.Batch(m.spin.Tick, runDoctor(m.course, m.profile))
		}
		return m, nil

	case key.Matches(msg, m.keys.Doctor):
		if m.course == nil {
			return m, nil
		}
		// doctor needs full metadata, which the opening scan does not read.
		m.busy = "probing"
		return m, tea.Batch(m.spin.Tick, fullScanThenDoctor(m.ctx, m.cfg, m.profile))

	case key.Matches(msg, m.keys.Loudness):
		if m.course == nil {
			return m, nil
		}
		m.busy = "measuring loudness"
		return m, tea.Batch(m.spin.Tick, measureLoudness(m.ctx, m.cfg, m.course))

	case key.Matches(msg, m.keys.Rename):
		if m.course == nil {
			return m, nil
		}
		m.busy = "planning"
		return m, tea.Batch(m.spin.Tick, buildPlan(m.course, m.cfg))

	case key.Matches(msg, m.keys.Apply):
		if m.screen != screenRename || m.plan == nil || m.plan.Empty() {
			return m, nil
		}
		if rename.HasFatal(m.problems) {
			m.status = statusLine{"cannot apply: resolve the blocking problems first", statusBad}
			return m, nil
		}
		m.confirming = true
		return m, nil

	case key.Matches(msg, m.keys.Undo):
		m.busy = "undoing"
		return m, tea.Batch(m.spin.Tick, undoLast(m.cfg.Root))

	case key.Matches(msg, m.keys.Export):
		if m.course == nil {
			return m, nil
		}
		m.busy = "exporting"
		return m, tea.Batch(m.spin.Tick, exportMarkdown(m.course))

	case key.Matches(msg, m.keys.Rescan):
		m.busy = "reading"
		m.status = statusLine{}
		return m, tea.Batch(m.spin.Tick, startScan(m.ctx, m.cfg, false))
	}

	return m, nil
}

// View renders the interface.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	// The alternate screen keeps the user's scrollback intact, so quitting
	// leaves their terminal exactly as they left it.
	v.AltScreen = true
	return v
}

func nextProfile(current string) string {
	for i, p := range profileCycle {
		if p == current {
			return profileCycle[(i+1)%len(profileCycle)]
		}
	}
	return profileCycle[0]
}

func statusKindFor(errs, warns int) statusKind {
	switch {
	case errs > 0:
		return statusBad
	case warns > 0:
		return statusInfo
	default:
		return statusGood
	}
}

// defaultExportName is where the e key writes its report.
func defaultExportName(c *model.Course) string {
	base := strings.TrimSpace(c.Title)
	if base == "" {
		base = "course"
	}
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, base)
	return filepath.Join(mustCwd(), safe+".md")
}

func mustCwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
