# coursekit

**A single binary that tells you what is actually in your folder of course recordings — and what will stop it being published.**

[![CI](https://github.com/ElAmir-Mansour/coursekit/actions/workflows/ci.yml/badge.svg)](https://github.com/ElAmir-Mansour/coursekit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/ElAmir-Mansour/coursekit.svg)](https://pkg.go.dev/github.com/ElAmir-Mansour/coursekit)
[![Go Report Card](https://goreportcard.com/badge/github.com/ElAmir-Mansour/coursekit)](https://goreportcard.com/report/github.com/ElAmir-Mansour/coursekit)
[![Release](https://img.shields.io/github/v/release/ElAmir-Mansour/coursekit?sort=semver)](https://github.com/ElAmir-Mansour/coursekit/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

---

## The problem

You record a course over a few months. By the end, nobody knows how many lessons
there are, how long the whole thing runs, or whether the files will pass the
platform's requirements. So you write the one-liner:

```sh
find . -name '*.mp4' -exec ffprobe -v quiet -of csv=p=0 -show_entries format=duration {} \; \
  | paste -sd+ - | bc
```

It gives you a number like `19684.44`. It is also slow, it silently skips the
files at the top level, it tells you nothing about whether the videos are
actually usable, and you will rewrite it from memory next month.

`coursekit` is that one-liner turned into a tool that answers the rest of the
question too.

```
$ coursekit scan ~/Desktop/"Go Backend Course"

Go Backend Course
/Users/you/Desktop/Go Backend Course

     #  CHAPTER                             LESSONS   DURATION        SIZE
     ·  (course root)                             1       3.1m     133 MiB
     1  Chap 1                                    6      52.8m     625 MiB
     2  Chap 2 Middleware                         4      45.2m     618 MiB
     3  Chpater 3 Postgres             ⚠          4      31.3m     571 MiB
     4  Chapter 4                                 3      37.3m     495 MiB
     5  Chapter 5 Deploys              ␣          3      25.4m     329 MiB
     6  Chapter 6 Observability                   4      58.1m     722 MiB
     7  Chapter 7 Caching                         2      11.7m     178 MiB
     8  Chapter 8 - Local Dev Setup               3      36.5m     431 MiB
     9  Chapter 9 - profiling                     1      11.3m     132 MiB
    --  Scratch recordings             ?          1       6.2m      42 MiB
    --  Intro takes and notes          ?          2       9.4m     269 MiB
──────────────────────────────────────────────────────────────────────────
        11 chapters                              34      5h28m     4.4 GiB

  3 attachments (38 MiB) · scanned in 15ms · 34 read in-process
  ? 2 folders have no chapter number: Scratch recordings, Intro takes and
  notes
```

Three things to notice:

- **15 milliseconds.** Duration comes out of the MP4 container header
  in-process. No subprocess per file.
- **Chapter 3 is in position 3**, even though it is spelled `Chpater`. An
  alphabetical sort puts it after chapter 9.
- **The flags.** `⚠` a misspelled chapter word · `␣` a trailing space you cannot
  see in Finder · `?` no chapter number at all.

## Install

**Homebrew**

```sh
brew install ElAmir-Mansour/tap/coursekit
```

**Go**

```sh
go install github.com/ElAmir-Mansour/coursekit/cmd/coursekit@latest
```

**Binaries** for macOS and Linux (Intel and ARM) are on the
[releases page](https://github.com/ElAmir-Mansour/coursekit/releases).

### Requirements

Go 1.26 or newer to build from source. That is all `scan` needs.

`ffmpeg` is **optional** and only needed for the deeper checks — `doctor` uses
`ffprobe` for codec and audio detail, and `--loudness` uses `ffmpeg`. Install it
with `brew install ffmpeg` or `apt install ffmpeg`. `coursekit version` reports
what it can find.

## Quickstart

```sh
coursekit                       # browse the current folder interactively
coursekit scan .                # how many lessons, how long
coursekit doctor .              # what will stop this being published
coursekit rename .              # fix the names that break ordering (dry run)
```

## The interactive browser

Running `coursekit` with no arguments opens a browser for the folder:

```
 Go Backend Course                    34 lessons · 5h 28m · 4.4 GiB
 /Users/you/Desktop/Go Backend Course

 ▸   ·  (course root)                        1    3.1m    95 MiB
 ▾   1  Chap 1                               6   52.8m   625 MiB
        goCourse-Intro chapter 1.mp4         ·    1.9m    32 MiB
        goCourse-why REST is dead.mp4      1.1   11.2m   131 MiB
        goCourse-context pyramid.mp4       1.2    8.4m    99 MiB
 ▸   2  Chap 2 Middleware                    4   45.2m   618 MiB
 ▸   3  Chpater 3 Postgres              ⚠    4   31.3m   571 MiB
 ▸   5  Chapter 5 Deploys               ␣    3   25.4m   329 MiB
 ▸  --  Scratch recordings              ?    1    6.2m    42 MiB

 34 lessons · 5h 28m
 ↑↓ move · ⏎ expand · a all · d doctor · r rename · s sort · / filter · ? help · q quit
```

| Key | |
|---|---|
| `↑ ↓` `j k` | move · `⏎` expand · `a` expand everything |
| `/` | filter by name (matches auto-expand) |
| `s` | sort: syllabus, duration, size, name |
| `d` | check against the current profile · `p` switch profile |
| `l` | measure loudness |
| `r` | build a rename plan · `c` apply it · `u` undo |
| `e` | write a Markdown report · `R` rescan · `?` help · `q` quit |

Piping the output gives you the plain table instead, so `coursekit | head` and
`coursekit > notes.txt` do the sensible thing rather than filling your file with
escape sequences.

## Commands

### `scan` — what is in here

```sh
coursekit scan .                    # aligned table
coursekit scan . --tree             # every lesson under its chapter
coursekit scan . --json | jq '.duration_human'
coursekit scan . --csv > lessons.csv
coursekit scan . -o course.md       # format inferred from the extension
coursekit scan . --full             # read everything with ffprobe
```

### `doctor` — what will stop this being published

```sh
coursekit doctor .                          # against Udemy's requirements
coursekit doctor . --profile lms            # against your own platform
coursekit doctor . --loudness               # include loudness (slower)
coursekit doctor . --verbose                # list every affected file
coursekit doctor . --json | jq '.findings[].rule'
```

Nothing is modified. Each finding carries the `ffmpeg` command that would fix
it, for you to run yourself:

```
  ✖ ERROR   Aspect ratio must be 16:9                              34 files
      Mac screen recordings default to 16:10, which is letterboxed or
      rejected by platforms that require 16:9.
      · Chap 1/goCourse-Intro chapter 1.mp4     1920x1200 (16:10)
      · Chap 1/goCourse-Tool selection 1.3.mp4  1920x1200 (16:10)
      · and 32 more (use --verbose to list them)
      fix ffmpeg -i IN -vf "scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:black" -c:a copy OUT

  ▲ WARN    Course mixes 2 audio sample rates                       2 files
      Mixed rates cause resampling artefacts and clicks when lessons are
      joined or an intro is prepended. Most of the course is 48000 Hz
      (32 files); the following differ.
      · Chapter 7 Caching/…-7.2.mp4              44100 Hz
      · Edited-Intro-short.mp4                    44100 Hz
      fix ffmpeg -i IN -c:v copy -c:a aac -ar 48000 OUT
```

The letterbox command is deliberate: cropping a screen recording to 16:9 would
cut off a menu bar or a dock, and you would not notice until a student did.

**Exit codes** — `0` clean, `1` the tool failed, `2` the course has errors. So
this works in CI:

```sh
coursekit doctor . --profile strict || echo "not ready to publish"
```

### `rename` — fix the names that break ordering

Dry run by default. Nothing moves until you pass `--commit`.

```sh
$ coursekit rename .

  Chapter folders
      Chap 1                           →  01
      Chap 2 Middleware                →  02 - Middleware
      Chpater 3 Postgres               →  03 - Postgres
      Chapter 5 Deploys                →  05 - Deploys
      Chapter 8 - Local Dev Setup      →  08 - Local Dev Setup

  Left alone
      Scratch recordings               no chapter number found in the folder name
      Intro takes and notes            no chapter number found in the folder name

  Dry run — nothing has been changed. Apply with coursekit rename --commit
```

Zero padding is the whole point: `03` sorts before `09` everywhere — Finder, a
web uploader, `ls`, a zip file.

```sh
coursekit rename . --commit                       # asks for confirmation first
coursekit rename . --files --strip-prefix         # rename lesson files too
coursekit rename . --template "{n:02}. {title}"   # your own scheme
coursekit undo .                                  # reverse it exactly
```

Template placeholders: `{n}` `{n:02}` `{ch}` `{lesson}` `{i}` `{title}` `{ext}`.

Folders with no detectable number are **never** renamed. There is no way to give
one a position without inventing it, so `coursekit` says so instead of guessing.

### `undo` — reverse the last rename

```sh
coursekit undo .            # reverse it
coursekit undo . --list     # rename history for this folder
```

Every committed rename writes a journal recording the exact moves that were
made. Undo replays that journal backwards. It does not re-derive the old names
from the templates, so an undo cannot be broken by a later change to the naming
rules.

### `export`, `profiles`, `version`

```sh
coursekit export . -o course.md         # or .csv / .json
coursekit profiles                      # what doctor can check against
coursekit profiles --show udemy         # a profile's annotated source
coursekit version                       # and whether ffmpeg was found
```

## Profiles

`doctor` checks against a profile. Four ship in the binary:

| Profile | What it is for |
|---|---|
| `udemy` | Udemy's published [video standards](https://business-support.udemy.com/hc/en-us/articles/360022570274-Video-Standards): strict 16:9, ≥1280×720, H.264/HEVC/ProRes, 25–60 fps, AAC ≥256 kbps stereo, MP4/MOV, ≤4 GB, ≤4 h |
| `youtube` | Shape is flexible; loudness is not. YouTube normalises playback toward about −14 LUFS, so a quiet upload just plays quieter than everything around it |
| `lms` | Your own site. **H.264 only** — HEVC plays in Safari and not reliably in Chrome or Firefox, so a self-hosted HEVC course fails for some students with no error message. Also caps file size for bandwidth and checks name portability |
| `strict` | The tightest of every rule, for a course intended to pass anywhere |

Every profile also runs **consistency** checks, which are the ones that catch
real problems: mixed resolutions, mixed sample rates, mixed codecs, loudness
spread across the course, and gaps or duplicates in lesson numbering.

### Your own profile

Profiles are YAML. Copy one and edit it:

```sh
mkdir -p ~/.config/coursekit/profiles
coursekit profiles --show lms > ~/.config/coursekit/profiles/mine.yaml
coursekit doctor . --profile mine
```

A file in that directory overrides a built-in of the same name. Full schema in
[docs/PROFILES.md](docs/PROFILES.md).

## Case study: a real five-hour course

These numbers are from an actual recorded course, scanned before it was
published. It is a normal, competently made course; every one of these was
invisible to its author.

| | |
|---|---|
| **34 lessons, 5h 28m, 4.4 GiB** across 11 chapter folders | |
| ✖ **All 34 videos were 16:10** (1920×1200 and 1728×1080) | Udemy requires 16:9. The entire course needed re-exporting |
| ✖ **Every file measured below −20 LUFS**, the quietest at −33.0 | Target is −14 to −16. Students would reach for the volume on every lesson |
| ▲ **Loudness varied by 12.4 LU** across the course | One chapter was recorded much quieter than the rest |
| ▲ **All 34 files had ~159 kbps audio** | Udemy asks for 256 kbps or better |
| ▲ **Two files were 44.1 kHz** while the other 32 were 48 kHz | Both were re-exports that used different settings |
| ▲ **Two files were 1728×1080** while the other 32 were 1920×1200 | The same two re-exports |
| ▲ **Five names would break on Windows or in a URL** | `?` and `:` in filenames, and a folder ending in a space |
| ▲ **Chapter 3's folder was spelled `Chpater`** | So it sorted after chapter 9 in Finder and in the uploader |

The loudness spread is the one worth dwelling on. Nobody notices that their
audio is 17 dB too quiet, because they listen to each lesson right after
recording it, at whatever volume their Mac happens to be at.

## How it works

### Duration without a subprocess

`scan` needs two things per file: duration and size. For MP4 and MOV that means
reading the `mvhd` and `tkhd` boxes, which `coursekit` does in-process with
[abema/go-mp4](https://github.com/abema/go-mp4). Measured over the 34-file
course above:

| Approach | Time | |
|---|---|---|
| **mvhd + tkhd, in-process** | **7 ms** | what `coursekit scan` does |
| `ffprobe` per file, sequential | 2,000 ms | ~280× slower |
| `go-mp4`'s own full `Probe()` | 10,300 ms | walks the sample tables; also does not recognise HEVC |

Anything the fast path cannot read — `.mkv`, `.webm`, a malformed MP4 — falls
back to `ffprobe` automatically. `doctor` always uses `ffprobe`, because codec,
bitrate and audio detail simply are not in the container header, and guessing at
them would produce confident nonsense.

### Loudness, and why it is opt-in

Measuring EBU R128 loudness means decoding the audio of every file. Two choices
make it bearable, both measured on one 18m53s HEVC screen recording:

| | Time |
|---|---|
| `ebur128` filter, video decoded too | 24.2 s |
| **`ebur128` with `-vn`** | **5.2 s** |
| `loudnorm` JSON output, with `-vn` | 34.5 s |

Passing `-vn` so the video stream is never decoded is a 4.6× win — without it,
ffmpeg burns five cores decoding pictures nobody looks at. And `ebur128` beats
`loudnorm`'s convenient JSON by 6.6×, because `loudnorm` performs a full
two-pass analysis.

Concurrency is capped at 4. Measured over 8 real files: 1 worker 3.32 s,
2 workers 1.92 s, **4 workers 1.57 s**, 8 workers 1.59 s. The filter is largely
single-threaded, so past 4 you are only adding contention.

The whole 5h 28m course measures in **26 seconds**. Results are cached, so you
pay that once.

### Reading through typos

Chapter order is parsed, not sorted. The leading word is matched against a list
of known chapter keywords with a Levenshtein distance allowance that scales with
word length, so `Chpater`, `Secton`, `Modle` and `Lecutre` all resolve — and the
next typo will too, without anyone adding it to a list.

A number behind an *unrecognised* word (`Swift 5 Basics`) is recorded but not
trusted for ordering, and a folder with no number is reported rather than
silently placed somewhere.

### Renames that can be undone

Renaming happens in two phases per directory: everything moves to a unique
temporary name, then from there to its final one.

This is not defensive padding. A single-phase loop cannot express a swap —
renaming A to B while B still exists either fails or destroys B — and it cannot
express a case-only rename on a case-insensitive filesystem either, because
`chap 1` and `Chap 1` are the *same directory entry* on APFS and NTFS. A naive
existence check reads that as a collision and refuses a rename that is perfectly
safe; `os.SameFile` settles it by identity instead of by name.

Contents are renamed before the folders holding them, and an undo reverses that
order, because a lesson's recorded path only resolves again once its chapter
folder has its original name back.

Before anything moves, the journal is written in a *pending* state. If the
process is killed half-way, that file is what makes the damage discoverable
instead of invisible. And if a rename fails part-way, every move already made is
reversed before the error is returned.

### Nothing is written to your course folder

The metadata cache lives in `~/Library/Caches/coursekit` (or
`$XDG_CACHE_HOME/coursekit`), keyed on path, size and modification time. So do
the rename journals. Scanning is something people expect to be read-only, and a
tool that scatters state through somebody's recordings is a tool they stop
trusting.

The only time `coursekit` writes into your course is `rename --commit`, and only
after showing you the plan and asking.

### What gets ignored

`.DS_Store`, AppleDouble sidecars (`._clip.mov`), dotfiles, `desktop.ini`,
`Thumbs.db`, zero-byte files (aborted exports), and editor scratch folders like
`Adobe Premiere Pro Video Previews` and `Proxy Media` — which are full of media
that would otherwise be counted as lessons.

PDFs, slide decks and worksheets are counted as **attachments**: reported
separately, never added to the runtime.

## Contributing

Bug reports and pull requests are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the build and test commands and a tour of
the layout.

```sh
git clone https://github.com/ElAmir-Mansour/coursekit
cd coursekit
go test ./...          # the fixture-backed tests need ffmpeg
go run ./cmd/coursekit scan .
```

The tests build a real course folder with ffmpeg — misspelled chapter, folder
with a trailing space, a sample-rate outlier, mixed aspect ratios, an aborted
zero-byte export — and run against that. Fixtures modelled on a real course
catch things that invented tidy names never will.

## License

[MIT](LICENSE)
