# Design

Why `coursekit` is built the way it is. Measurements behind the performance
decisions are in [RESEARCH.md](RESEARCH.md).

## What it is for

A course lives in a folder of screen recordings for months before it is
published. Over that time the basics get lost: how many lessons there are, how
long the whole thing runs, whether the chapters are even in a defined order, and
whether any of it will pass the platform's requirements.

The existing answer is a `find | ffprobe | bc` one-liner. It totals durations.
`coursekit` exists to answer the rest of the question, and to be fast enough
that checking is something you do casually rather than something you plan.

## Shape of the program

```
cmd/coursekit          main
internal/
  model/               data + the name parser. no dependencies
  scan/                walk, metadata, cache, loudness
  lint/                rule engine + embedded profile YAML
  rename/              plan, validate, apply, journal, undo
  export/              table, JSON, CSV, Markdown, doctor renderers
  tui/                 interactive browser
  coursefixture/       builds a throwaway course for tests
```

Dependencies point inward. `model` imports nothing; `scan` imports `model`;
`lint`, `rename` and `export` import `model` and `scan`; `tui` and `cli` sit on
top. Nothing in `internal/model` knows that a terminal exists.

`model` is dependency-free on purpose. The chapter and lesson parser is the
trickiest code here, and it is worth being able to test it with nothing but the
standard library.

## Two tiers of metadata

`scan` and `doctor` need different things, and conflating them would make the
common case pay for the rare one.

| | needs | how |
|---|---|---|
| `scan` | duration, size, dimensions | `mvhd` + `tkhd`, in-process |
| `doctor` | codec, bitrate, fps, audio layout, container | `ffprobe` per file |
| `--loudness` | EBU R128 | `ffmpeg` per file |

The gap is 7 ms against 2,000 ms across 34 files, so `scan` reads two named
boxes out of the container and never spawns anything. Files the fast path cannot
handle — `.mkv`, `.webm`, a malformed MP4 — fall through to `ffprobe`
automatically, so support is a performance question rather than a correctness
one.

Fast-path results are marked `Full: false`, and every rule that depends on codec
or audio data checks that flag first. A rule that fired on absent data would
produce confident nonsense, which is worse than staying quiet.

`ffmpeg` is therefore an **optional** dependency. `scan` works without it;
`doctor` explains what it needs and points at `scan`.

## Ordering is parsed, not sorted

The whole reason this tool exists in the shape it does:

```
Chap 1              Chap 1
Chap 2 Middleware   Chap 2 Middleware
Chapter 4           Chpater 3 Postgres     <- parsed
Chapter 5 …         Chapter 4
…                   Chapter 5 …
Chpater 3 Postgres    …
   ^ sorted
```

The parser trims whitespace, matches an optional leading word against known
chapter keywords with a Levenshtein allowance that scales with word length, and
extracts the number. So `Chpater`, `Secton`, `Modle` and `Lecutre` all resolve,
and the next typo will too without anyone adding it to a list.

Three confidence levels, because guessing silently is the failure mode to avoid:

- **strong** — a bare leading number, or a recognised keyword. Drives ordering.
- **weak** — a number behind an unrecognised word (`Swift 5 Basics`). Recorded,
  not trusted for ordering, and never renamed.
- **none** — no number. Sorted last, flagged in the output, never renamed.

The short keywords (`ch`, `ep`) get an edit budget of zero, or every folder
beginning with two letters becomes a chapter.

## Findings aggregate by rule

"34 files are the wrong shape" is one problem to solve, not thirty-four. Every
finding carries the rule, a severity, the affected files with what was measured
about each, and the `ffmpeg` command that would fix it.

Consistency rules name only the **minority** groups. Listing the 32 files that
are fine alongside the 2 that are not would bury the answer.

Fix commands are printed and never run. `coursekit` does not re-encode anything
in this version: transcoding somebody's only master is not a thing to do as a
side effect of a check.

The suggested aspect-ratio fix letterboxes rather than crops. Cropping a screen
recording to 16:9 removes a menu bar or a dock, and the author would not notice
until a student did.

## Renaming safely

The only part that modifies anything, so:

**Dry run is the default.** `--commit` is required, and prompts unless `--yes`.
A piped invocation with no terminal refuses rather than proceeding, because
there is nobody there to answer.

**Two phases per directory.** Everything moves to a unique temporary name, then
from there to its final one. Not padding — a single-phase loop cannot express a
swap (renaming A to B while B exists either fails or destroys B), and it cannot
express a case-only rename on a case-insensitive filesystem, where `chap 1` and
`Chap 1` are the same directory entry. `os.SameFile` distinguishes "this target
is really me" from "this target is something else" by identity rather than name.

**Validation before mutation.** Missing sources, empty names, path separators,
over-long names, cross-directory moves, two-sources-to-one-target, and targets
differing only in case. A target that exists is allowed when it is itself a
source elsewhere in the plan, which is what makes a rotation expressible.

**Depth ordering.** Contents are renamed before the folders holding them. A
parent renamed first would invalidate every child path still queued. Validation
asserts this ordering rather than trusting the plan builder to have got it right.

**A journal, written before anything moves.** It is written in a *pending*
state first, so a process killed half-way leaves a discoverable record instead
of silent damage. Undo replays the journal backwards through the same two-phase
engine, rather than re-deriving names from templates — a later change to the
naming rules cannot break an old undo.

The reversal order matters: chapter folders were renamed last, so they are
restored first. A lesson's recorded path only resolves again once its parent has
its original name back. Existence is therefore checked one directory group at a
time, not up front.

**Rollback.** A failure part-way through reverses every move already made and
deletes the journal, so there is never a journal describing work that no longer
exists.

**Nothing is renamed that cannot be numbered confidently.** Unnumbered folders
are reported as skipped, with the reason.

## Nothing is written to the course folder

The metadata cache and the rename journals live under
`~/Library/Caches/coursekit` (or `$XDG_CACHE_HOME`), keyed by a hash of the
absolute course path.

Scanning is something people expect to be read-only. A tool that drops state
files through somebody's recordings is a tool they stop pointing at their real
work. The only write into a course folder is `rename --commit`.

The cache is keyed on path, size and modification time, so an edited file is
never served stale. It is also an optimisation and never a correctness
dependency: a corrupt or unreadable cache is treated as empty, and a cache that
cannot be written does not fail a scan.

A full record satisfies a fast query, but not the reverse — so running `doctor`
and then `scan` gives the richer data for free, while `scan` followed by
`doctor` still re-probes properly.

## The interface

Bubble Tea, Elm architecture. Three things are worth calling out.

**Slow work never touches `Update`.** Scanning, probing, loudness, planning,
applying and undoing all run as commands and report back as messages. Progress
arrives over a buffered channel whose pushes are non-blocking and drop when
full: progress is cosmetic, and must never stall the scan producing it.

**The non-terminal fallback is a correctness requirement.** Without it,
`coursekit > notes.txt` fills a file with cursor-control sequences. The same
check governs colour everywhere, with `NO_COLOR` and `CLICOLOR_FORCE` honoured.

**Layout is measured, not estimated.** Columns are described by a layout type
that computes its own total width, and drops the size column and then the
duration column rather than overflowing. Both layout bugs found during
development were found by tests asserting that no rendered line exceeds the
terminal width at 60, 80, 100 and 200 columns.

Colours resolve through `lipgloss.LightDark` against the background colour the
terminal reports at startup, because half of all users are on a light theme.

## Deliberate non-goals

- **No re-encoding.** `doctor` prints the command; you run it. A future `fix`
  command would need its own careful design around backups and verification.
- **No moving files between directories.** Renaming stays within a parent.
- **No upload or platform APIs.** Profiles encode published requirements; that
  is the useful, stable part.
- **No playback.**
