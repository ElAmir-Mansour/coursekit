package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElAmir-Mansour/coursekit/internal/coursefixture"
)

// runCmd executes the command tree with the given arguments and captures
// stdout, so the wiring between flags and behaviour is covered rather than
// only the packages underneath it.
func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Command construction reads package-level flag state, so reset it between
	// runs to keep tests independent of each other's ordering.
	global = globalFlags{}

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(w)
	root.SetErr(w)

	runErr := root.ExecuteContext(context.Background())

	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, copyErr := buf.ReadFrom(r); copyErr != nil {
		t.Fatalf("read captured output: %v", copyErr)
	}
	return buf.String(), runErr
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return coursefixture.Build(t)
}

func TestScanCmd_Table(t *testing.T) {
	root := fixtureRoot(t)

	out, err := runCmd(t, "scan", root, "--no-color", "--no-cache")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	for _, want := range []string{"Go Backend Course", "Chpater 3 Postgres", "5 chapters", "9"} {
		if !strings.Contains(out, want) {
			t.Errorf("scan output is missing %q\n%s", want, out)
		}
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Error("--no-color output contains an escape sequence")
	}
}

func TestScanCmd_JSON(t *testing.T) {
	root := fixtureRoot(t)

	out, err := runCmd(t, "scan", root, "--json", "--no-cache")
	if err != nil {
		t.Fatalf("scan --json: %v", err)
	}

	var got struct {
		Lessons  int     `json:"lessons"`
		Duration float64 `json:"duration_seconds"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got.Lessons != len(coursefixture.Layout) {
		t.Errorf("lessons = %d, want %d", got.Lessons, len(coursefixture.Layout))
	}
}

func TestScanCmd_RejectsTwoFormats(t *testing.T) {
	root := fixtureRoot(t)

	if _, err := runCmd(t, "scan", root, "--json", "--csv"); err == nil {
		t.Error("expected an error when two output formats are requested")
	}
}

func TestScanCmd_InfersFormatFromOutputPath(t *testing.T) {
	root := fixtureRoot(t)
	out := filepath.Join(t.TempDir(), "report.csv")

	if _, err := runCmd(t, "scan", root, "-o", out, "--no-cache"); err != nil {
		t.Fatalf("scan -o: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}
	if !strings.HasPrefix(string(data), "chapter,chapter_order,") {
		t.Errorf("a .csv path should produce CSV, got:\n%s", firstLine(string(data)))
	}
}

// A path that is a file rather than a directory is a common mistake, and the
// message has to say so.
func TestResolveRoot_RejectsAFile(t *testing.T) {
	root := fixtureRoot(t)
	file := filepath.Join(root, "Edited-Intro-short.mp4")

	_, err := runCmd(t, "scan", file)
	if err == nil {
		t.Fatal("expected an error when pointed at a file")
	}
	if !strings.Contains(err.Error(), "course folder") {
		t.Errorf("error = %v, want it to explain that a folder is needed", err)
	}
}

func TestScanCmd_EmptyFolderIsExplained(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	empty := t.TempDir()

	out, err := runCmd(t, "scan", empty, "--no-color")
	if err != nil {
		t.Fatalf("scan on an empty folder: %v", err)
	}
	if !strings.Contains(out, "No video or audio files found") {
		t.Errorf("an empty folder should be explained, got:\n%s", out)
	}
}

// doctor must exit 2 when the course has errors, since that is the contract
// scripts and CI depend on.
func TestDoctorCmd_ExitCodeOnErrors(t *testing.T) {
	root := fixtureRoot(t)

	_, err := runCmd(t, "doctor", root, "--no-color", "--no-cache")
	if err == nil {
		t.Fatal("expected doctor to fail on a course with errors")
	}
	if got := exitCodeFor(err); got != exitCourseHasErrors {
		t.Errorf("exit code = %d, want %d", got, exitCourseHasErrors)
	}
	if !isQuiet(err) {
		t.Error("the error should be quiet: doctor already printed its report")
	}
}

func TestDoctorCmd_NoFailSuppressesExitCode(t *testing.T) {
	root := fixtureRoot(t)

	if _, err := runCmd(t, "doctor", root, "--no-fail", "--no-color", "--no-cache"); err != nil {
		t.Errorf("--no-fail should exit 0, got %v", err)
	}
}

func TestDoctorCmd_UnknownProfile(t *testing.T) {
	root := fixtureRoot(t)

	_, err := runCmd(t, "doctor", root, "--profile", "teachable")
	if err == nil {
		t.Fatal("expected an error for an unknown profile")
	}
	// The message has to list what is available, or the user is left guessing.
	if !strings.Contains(err.Error(), "udemy") {
		t.Errorf("error = %v, want it to list the built-in profiles", err)
	}
}

func TestRenameCmd_DryRunChangesNothing(t *testing.T) {
	root := fixtureRoot(t)

	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	out, runErr := runCmd(t, "rename", root, "--no-color", "--no-cache")
	if runErr != nil {
		t.Fatalf("rename: %v", runErr)
	}

	if !strings.Contains(out, "Dry run") {
		t.Errorf("output must say it is a dry run:\n%s", out)
	}
	if !strings.Contains(out, "03 - Postgres") {
		t.Errorf("output should show the proposed name:\n%s", out)
	}

	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("entry count changed: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Name() != after[i].Name() {
			t.Errorf("a dry run renamed %q to %q", before[i].Name(), after[i].Name())
		}
	}
}

// Without a terminal there is nobody to answer a prompt, so a piped rename
// must refuse rather than proceed silently.
func TestRenameCmd_RefusesToCommitWithoutATerminal(t *testing.T) {
	root := fixtureRoot(t)

	_, err := runCmd(t, "rename", root, "--commit", "--no-color", "--no-cache")
	if err == nil {
		t.Fatal("expected --commit to refuse without a terminal or --yes")
	}
	if !strings.Contains(err.Error(), "confirmation") {
		t.Errorf("error = %v, want it to mention the missing confirmation", err)
	}

	if _, statErr := os.Lstat(filepath.Join(root, "Chpater 3 Postgres")); statErr != nil {
		t.Error("the refused rename modified the course anyway")
	}
}

func TestRenameCmd_CommitThenUndo(t *testing.T) {
	root := fixtureRoot(t)

	if _, err := runCmd(t, "rename", root, "--commit", "--yes", "--no-color", "--no-cache"); err != nil {
		t.Fatalf("rename --commit: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "03 - Postgres")); err != nil {
		t.Fatalf("expected the renamed folder to exist: %v", err)
	}

	out, err := runCmd(t, "undo", root)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !strings.Contains(out, "Reversed") {
		t.Errorf("undo should report what it reversed:\n%s", out)
	}
	if _, err := os.Lstat(filepath.Join(root, "Chpater 3 Postgres")); err != nil {
		t.Errorf("undo did not restore the original name: %v", err)
	}

	// A second undo must refuse rather than reverse the same journal twice.
	if _, err := runCmd(t, "undo", root); err == nil {
		t.Error("a second undo should report that there is nothing left to reverse")
	}
}

func TestUndoCmd_NothingRecorded(t *testing.T) {
	root := fixtureRoot(t)

	_, err := runCmd(t, "undo", root)
	if err == nil {
		t.Fatal("expected an error when nothing has been renamed")
	}
	if !strings.Contains(err.Error(), "no rename to undo") {
		t.Errorf("error = %v, want it to say there is nothing to undo", err)
	}
}

func TestExportCmd(t *testing.T) {
	root := fixtureRoot(t)
	out := filepath.Join(t.TempDir(), "course.md")

	if _, err := runCmd(t, "export", root, "-o", out, "--no-cache"); err != nil {
		t.Fatalf("export: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read %s: %v", out, err)
	}
	if !strings.Contains(string(data), "# Go Backend Course") {
		t.Errorf("markdown report is missing its heading:\n%s", firstLine(string(data)))
	}
}

func TestExportCmd_RequiresKnownExtension(t *testing.T) {
	root := fixtureRoot(t)

	if _, err := runCmd(t, "export", root, "-o", filepath.Join(t.TempDir(), "report.txt")); err == nil {
		t.Error("expected an error for an unrecognised output extension")
	}
	if _, err := runCmd(t, "export", root); err == nil {
		t.Error("expected an error when no output path is given")
	}
}

func TestProfilesCmd(t *testing.T) {
	out, err := runCmd(t, "profiles")
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	for _, want := range []string{"udemy", "youtube", "lms", "strict"} {
		if !strings.Contains(out, want) {
			t.Errorf("profiles output is missing %q", want)
		}
	}

	shown, err := runCmd(t, "profiles", "--show", "udemy")
	if err != nil {
		t.Fatalf("profiles --show: %v", err)
	}
	// The source is printed rather than a re-marshalled struct, so the
	// comments explaining each limit survive.
	if !strings.Contains(shown, "name: udemy") || !strings.Contains(shown, "#") {
		t.Errorf("--show should print the annotated YAML source:\n%s", shown)
	}
}

func TestVersionCmd(t *testing.T) {
	out, err := runCmd(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, want := range []string{"coursekit", "go", "target", "cache"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output is missing %q\n%s", want, out)
		}
	}
}

func TestExitCodeFor(t *testing.T) {
	if got := exitCodeFor(errors.New("boom")); got != 1 {
		t.Errorf("a plain error should exit 1, got %d", got)
	}
	if got := exitCodeFor(withExitCode(7, errors.New("boom"))); got != 7 {
		t.Errorf("exit code = %d, want 7", got)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
