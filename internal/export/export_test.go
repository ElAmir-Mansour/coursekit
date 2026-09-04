package export_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/lint"
	"github.com/ElAmir-Mansour/coursekit/internal/model"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

func fixtureCourse(t *testing.T, full bool) *model.Course {
	t.Helper()
	root := coursefixture.Build(t)
	res, err := scan.Scan(context.Background(), scan.Options{
		Root: root, Full: full, Cache: scan.NoCache(),
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return res.Course
}

// Plain output must be free of escape sequences, or a redirected report is
// full of terminal noise.
func TestTable_NoColorHasNoEscapes(t *testing.T) {
	c := fixtureCourse(t, false)

	var buf bytes.Buffer
	if err := export.Table(&buf, c, export.TableOptions{
		Palette: export.NoColor, Width: 100, ShowPath: true, Tree: true,
	}); err != nil {
		t.Fatalf("Table: %v", err)
	}

	if strings.ContainsRune(buf.String(), 0x1b) {
		t.Error("plain table output contains an escape sequence")
	}
	for _, want := range []string{"Go Backend Course", "Chpater 3 Postgres", "Chapter 5 Deploys", "9"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("table is missing %q", want)
		}
	}
}

// Every line has to fit the declared width, at every width.
func TestTable_RespectsWidth(t *testing.T) {
	c := fixtureCourse(t, false)

	for _, w := range []int{60, 80, 100, 160} {
		var buf bytes.Buffer
		if err := export.Table(&buf, c, export.TableOptions{
			Palette: export.NoColor, Width: w, Tree: true,
		}); err != nil {
			t.Fatalf("Table: %v", err)
		}
		for i, line := range strings.Split(buf.String(), "\n") {
			if len([]rune(line)) > w {
				t.Errorf("width %d: line %d is %d columns:\n%q", w, i, len([]rune(line)), line)
			}
		}
	}
}

func TestJSON_RoundTrips(t *testing.T) {
	c := fixtureCourse(t, false)

	var buf bytes.Buffer
	if err := export.JSON(&buf, c); err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got export.Summary
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if got.Lessons != len(coursefixture.Layout) {
		t.Errorf("lessons = %d, want %d", got.Lessons, len(coursefixture.Layout))
	}
	if got.Attachments != 1 {
		t.Errorf("attachments = %d, want 1", got.Attachments)
	}
	if got.DurationSec < 13 || got.DurationSec > 15 {
		t.Errorf("duration = %.1fs, want about %ds", got.DurationSec, coursefixture.TotalSeconds())
	}

	// Chapter order must survive serialisation, since that is the whole point.
	var names []string
	for _, ch := range got.Detail {
		names = append(names, ch.Name)
	}
	want := []string{"(course root)", "Chap 1", "Chap 2 Middleware", "Chpater 3 Postgres", "Chapter 5 Deploys ", "Scratch recordings"}
	if len(names) != len(want) {
		t.Fatalf("chapters = %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("chapter[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestCSV_IsParseableAndComplete(t *testing.T) {
	c := fixtureCourse(t, true)

	var buf bytes.Buffer
	if err := export.CSV(&buf, c); err != nil {
		t.Fatalf("CSV: %v", err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}

	if got, want := len(records)-1, len(coursefixture.Layout); got != want {
		t.Errorf("CSV has %d data rows, want %d", got, want)
	}

	header := records[0]
	for _, want := range []string{"chapter", "duration_seconds", "resolution", "aspect", "integrated_lufs"} {
		var found bool
		for _, h := range header {
			if h == want {
				found = true
			}
		}
		if !found {
			t.Errorf("CSV header is missing %q", want)
		}
	}

	// A field containing a comma or quote must not break the row structure;
	// encoding/csv handles it, and reading it back proves it.
	for i, row := range records {
		if len(row) != len(header) {
			t.Errorf("row %d has %d fields, want %d", i, len(row), len(header))
		}
	}
}

func TestMarkdown_HasStructureAndEscapesPipes(t *testing.T) {
	c := fixtureCourse(t, false)

	var buf bytes.Buffer
	if err := export.Markdown(&buf, c); err != nil {
		t.Fatalf("Markdown: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"# Go Backend Course", "## Chapters", "## Lessons", "| Total runtime |"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown is missing %q", want)
		}
	}

	// A pipe in a filename would otherwise break the table it sits in.
	if strings.Contains(out, "a|b") {
		t.Error("an unescaped pipe survived into the markdown table")
	}
}

func TestDoctor_PlainOutput(t *testing.T) {
	c := fixtureCourse(t, true)
	profile, err := lint.LoadProfile("udemy")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	rep := lint.Check(c, profile)

	var buf bytes.Buffer
	if err := export.Doctor(&buf, c, rep, export.DoctorOptions{
		Palette: export.NoColor, Width: 100,
	}); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	out := buf.String()

	if strings.ContainsRune(out, 0x1b) {
		t.Error("plain doctor output contains an escape sequence")
	}
	for _, want := range []string{"ERROR", "Aspect ratio must be 16:9", "fix", "files checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output is missing %q", want)
		}
	}
	if !strings.Contains(out, "loudness not measured") {
		t.Error("doctor output should say loudness was not measured")
	}
}

func TestDoctorJSON_IsValid(t *testing.T) {
	c := fixtureCourse(t, true)
	profile, _ := lint.LoadProfile("udemy")
	rep := lint.Check(c, profile)

	var buf bytes.Buffer
	if err := export.DoctorJSON(&buf, rep); err != nil {
		t.Fatalf("DoctorJSON: %v", err)
	}

	var got struct {
		Profile  string `json:"profile"`
		OK       bool   `json:"ok"`
		Errors   int    `json:"errors"`
		Findings []struct {
			Rule         string `json:"rule"`
			SeverityName string `json:"severity_name"`
			FileCount    int    `json:"file_count"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	if got.Profile != "udemy" {
		t.Errorf("profile = %q", got.Profile)
	}
	if got.OK {
		t.Error("ok = true, but the fixture has errors")
	}
	if got.Errors < 1 {
		t.Errorf("errors = %d, want at least 1", got.Errors)
	}
	for _, f := range got.Findings {
		if f.FileCount < 1 {
			t.Errorf("finding %q covers no files", f.Rule)
		}
		if f.SeverityName == "" {
			t.Errorf("finding %q has no severity name", f.Rule)
		}
	}
}

// A course pointed at an empty folder must produce a coherent report rather
// than a table of nothing or a division by zero.
func TestTable_EmptyCourse(t *testing.T) {
	c := &model.Course{Root: "/tmp/empty", Title: "empty"}

	var buf bytes.Buffer
	if err := export.Table(&buf, c, export.TableOptions{
		Palette: export.NoColor, Width: 80,
	}); err != nil {
		t.Fatalf("Table on an empty course: %v", err)
	}
	if !strings.Contains(buf.String(), "0 chapters") {
		t.Errorf("empty course should report zero chapters:\n%s", buf.String())
	}
}

func TestPaletteFor_HonoursNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if got := export.PaletteFor(nil, false); got != export.NoColor {
		t.Error("NO_COLOR should disable colour")
	}
	// An explicit request still wins, which is what CLICOLOR_FORCE is for.
	if got := export.PaletteFor(nil, true); got == export.NoColor {
		t.Error("an explicit colour request should override NO_COLOR")
	}
}
