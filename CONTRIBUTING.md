# Contributing to coursekit

Bug reports, feature requests and pull requests are all welcome.

## Getting set up

```sh
git clone https://github.com/ElAmir-Mansour/coursekit
cd coursekit
go build ./...
go run ./cmd/coursekit scan .
```

You need **Go 1.26+**. You also want **ffmpeg** on your PATH: the tests
generate real video files with it, and skip themselves if it is missing, which
means a run without ffmpeg will pass while testing much less than you think.

```sh
brew install ffmpeg     # macOS
apt install ffmpeg      # Debian/Ubuntu
```

## The commands you will actually run

```sh
go test ./...                      # everything
go test -race ./...                # there are three worker pools; run this
go test ./internal/rename/ -v      # the destructive path, in detail
go vet ./...
gofmt -l .                         # must print nothing
```

Before opening a pull request: `gofmt -l .` clean, `go vet ./...` clean, and
`go test -race ./...` passing.

## How the code is laid out

```
cmd/coursekit/       main, three lines of it
internal/
  model/             pure data and the name parser — no dependencies at all
  scan/              walking the tree, reading metadata, caching, loudness
  lint/              the rule engine, plus the profiles as embedded YAML
  rename/            plan, validate, apply, journal, undo
  export/            table, JSON, CSV, Markdown, and the doctor renderers
  tui/               the interactive browser
  coursefixture/     builds a throwaway course folder for tests
```

`internal/model` deliberately has **no dependencies**. The chapter and lesson
parser lives there, it is the trickiest code in the project, and keeping it
dependency-free keeps it trivially testable.

## Testing philosophy

The fixtures are modelled on a real recorded course, and they are deliberately
untidy: a misspelled chapter folder (`Chpater 3 Postgres`), a folder with a
trailing space, unnumbered folders, wildly inconsistent lesson numbering, a
sample-rate outlier, mixed aspect ratios, an AppleDouble sidecar and an aborted
zero-byte export.

Please keep it that way. Every one of those cases came from real data, and
fixtures with invented tidy names would have caught none of them.

Two habits worth copying:

- **Prefer relative assertions to magic numbers** where a measurement is
  involved. The loudness test attenuates a tone by 20 dB and asserts the
  measurement moves by about 20 LU. Asserting one absolute LUFS figure would
  only pin down the local ffmpeg build's AAC encoder.
- **Assert the layout, not just the content.** There are tests that check no
  rendered line exceeds the terminal width at 60, 80, 100 and 200 columns. Both
  of the layout bugs found during development were found by those tests.

## Changing the rename engine

This is the only part of `coursekit` that modifies anything, so it has the
highest bar.

Anything you change here must keep these passing, and they are all in
`internal/rename/rename_test.go`:

- an apply-then-undo round trip restores the tree **exactly**, including the
  trailing space and the typo
- a swap (`A→B`, `B→A`) works in both directions
- a case-only rename works on a case-insensitive filesystem
- a failure part-way through rolls back completely and leaves **no journal**
- contents are always renamed before the folders holding them

If you find yourself wanting to remove the two-phase temporary rename, read
the comment on `runOps` first — a single-phase loop cannot express a swap or a
case-only rename, and both occur in practice.

## Adding a lint rule

1. Add the field to the right struct in `internal/lint/profile.go`.
2. Add the check to the matching `check*` function in `internal/lint/lint.go`.
3. Add the limit to the profiles in `internal/lint/profiles/` that should
   enforce it — **with a comment saying why that number**. A limit nobody can
   justify is a limit nobody can tune.
4. Add a case to `internal/lint/lint_test.go`.

A finding should carry a `fix` command whenever one exists. Reporting a problem
without saying how to solve it is only half the job.

Aggregate by rule, not by file: "34 files are the wrong shape" is one problem to
solve, not thirty-four.

## Adding a profile

Drop a YAML file in `internal/lint/profiles/`. It is picked up by `go:embed`
automatically and appears in `coursekit profiles`. Set `reference` to the
platform's own documentation, and when a limit is your judgement rather than
theirs, say so in a comment — the `udemy` profile does this for its loudness
target, which Udemy does not actually publish.

## Commit messages

Short imperative subject, and a body explaining *why* when it is not obvious:

```
Cap loudness workers at 4

Measured over 8 real course files: 1 worker 3.32s, 2 workers 1.92s,
4 workers 1.57s, 8 workers 1.59s. The ebur128 filter is largely
single-threaded, so past 4 workers we only add contention.
```
