# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-04

First release.

### Added

- `scan` — lesson counts, per-chapter and total runtime, size on disk, with
  `--tree`, `--json`, `--csv`, `--md` and `-o`.
- `doctor` — checks a course against a publishing profile: aspect ratio,
  resolution, codec, frame rate, bitrate, audio codec and layout, container,
  file size, duration, and name portability. Every finding carries the `ffmpeg`
  command that would fix it. Nothing is modified.
- Cross-file consistency checks: mixed resolutions, sample rates, frame rates
  and codecs; loudness spread; gaps and duplicates in lesson numbering.
- `--loudness` — EBU R128 measurement, opt-in and cached.
- `rename` — normalises chapter folder names, and lesson files with `--files`.
  Dry run by default; `--commit` writes an undo journal.
- `undo` — reverses the last committed rename exactly, with `--list` for history.
- `export` — Markdown, CSV or JSON report, format inferred from the extension.
- `profiles` — lists rule sets; `--show` prints a profile's annotated source.
- Four built-in profiles: `udemy`, `youtube`, `lms`, `strict`. User profiles in
  `~/.config/coursekit/profiles` override built-ins of the same name.
- Interactive browser (the default with no arguments): collapsible chapters,
  sorting, filtering, and the doctor and rename views. Falls back to a plain
  table when output is not a terminal.
- In-process MP4/MOV duration reading, roughly 280× faster than one `ffprobe`
  per file, with automatic fallback for other containers.
- Chapter-number parsing that tolerates misspelled keywords (`Chpater`,
  `Secton`, `Modle`) by fuzzy matching, so chapter 3 no longer sorts after
  chapter 9.
- Metadata cache keyed on path, size and modification time, kept outside the
  course folder.

[Unreleased]: https://github.com/ElAmir-Mansour/coursekit/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ElAmir-Mansour/coursekit/releases/tag/v0.1.0
