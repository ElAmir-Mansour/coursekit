# Writing a profile

A profile is the rule set `coursekit doctor` checks a course against. Four ship
in the binary — `udemy`, `youtube`, `lms`, `strict` — and you can write your own.

## Getting started

Copy a built-in and edit it. Start from the one closest to what you want:

```sh
mkdir -p ~/.config/coursekit/profiles
coursekit profiles --show lms > ~/.config/coursekit/profiles/mine.yaml
coursekit doctor . --profile mine
```

A file in that directory **overrides a built-in of the same name**, so naming
yours `udemy.yaml` replaces the shipped one entirely.

You can also point at a file directly, which is useful for a one-off:

```sh
coursekit doctor . --profile ./client-requirements.yaml
```

`$XDG_CONFIG_HOME` is honoured if set; otherwise the directory is
`~/.config/coursekit/profiles`.

## Rules of the file

- Unknown keys are an **error**, not a warning. A typo in a rule name would
  otherwise silently disable that rule, and you would trust a check that never
  ran.
- Omitting a key disables that check. An empty list means "anything goes".
- A `0` value also disables a numeric check, rather than meaning "must be zero".

## Full schema

```yaml
name: my-platform          # optional; the filename is used if absent
description: One line, shown in `coursekit profiles`
reference: https://…       # linked in the report; use the platform's own docs

video:
  # Allowed shapes. An empty list accepts any aspect ratio.
  # A bare decimal also works: ["1.85"]
  aspect_ratios: ["16:9"]
  # How far a measured ratio may drift and still match. Default 0.012, which
  # separates 16:9 from 16:10 comfortably while absorbing odd frame sizes.
  aspect_tolerance: 0.012

  min_width: 1280          # below this is an error
  min_height: 720
  recommended_width: 1920  # below this is a note
  recommended_height: 1080

  # ffprobe codec names, matched case-insensitively:
  # h264, hevc, av1, vp9, prores, mpeg4
  codecs: [h264, hevc]

  min_fps: 25
  max_fps: 60

  min_bitrate_kbps: 1000            # below this is a warning
  recommended_bitrate_kbps: 6000    # used in the suggested fix command

audio:
  required: true           # a lesson with no audio track is an error
  codecs: [aac, pcm_s16le]

  min_bitrate_kbps: 256
  recommended_bitrate_kbps: 256
  channels: [2]            # allowed channel counts
  sample_rates: [44100, 48000]

  # Programme loudness. Only checked when loudness has been measured, so
  # these rules stay silent unless you pass --loudness.
  target_lufs: -16.0
  lufs_tolerance: 3.0      # beyond this is a warning; beyond +6 more, an error
  max_true_peak_dbtp: -1.0

file:
  containers: [mp4, mov]
  max_size_bytes: 4294967296        # 4 GiB
  max_duration_seconds: 14400       # 4 hours

  # Flag names that are legal on macOS but break elsewhere: leading or
  # trailing spaces, < > : " | ? *, trailing dots, Windows device names
  # (CON, PRN, LPT1…), and very long names.
  portable_names: true
  # Additionally flag non-ASCII names. Off by default — they are usually fine,
  # but some uploaders and archive tools still corrupt them.
  ascii_only: false

consistency:
  # These compare files against each other rather than against a fixed limit,
  # and they are the rules that catch real problems. Only the minority groups
  # are named in the report, so two odd files out are not buried under thirty
  # that are fine.
  uniform_resolution: true
  uniform_sample_rate: true
  uniform_frame_rate: true
  uniform_codec: false
  max_loudness_spread_lu: 4.0       # 0 disables
  numbering_gaps: true              # holes and duplicates in "3.2" numbering
  chapter_ordering: true            # typos, stray whitespace, missing numbers
```

## Severities

| | Meaning |
|---|---|
| **error** | The platform will reject this, or it will not play. Makes `doctor` exit `2` |
| **warn** | It will be accepted, and the student's experience is worse for it |
| **note** | Worth knowing; blocks nothing |

Loudness escalates on its own: past `lufs_tolerance` is a warning, and more than
6 LU past it is an error. Being 2 dB quiet is a nuisance; being 17 dB quiet is
broken.

## Worked example: a client's delivery spec

A client wants 1080p H.264 in MP4, stereo audio at −16 LUFS, under 500 MB per
lesson, and lessons no longer than 12 minutes.

```yaml
name: client-acme
description: Acme delivery specification, revision 4
reference: https://acme.example/vendors/video-spec

video:
  aspect_ratios: ["16:9"]
  min_width: 1920
  min_height: 1080
  recommended_width: 1920
  recommended_height: 1080
  codecs: [h264]
  min_fps: 25
  max_fps: 30
  min_bitrate_kbps: 4000
  recommended_bitrate_kbps: 8000

audio:
  required: true
  codecs: [aac]
  min_bitrate_kbps: 192
  recommended_bitrate_kbps: 256
  channels: [2]
  sample_rates: [48000]
  target_lufs: -16.0
  lufs_tolerance: 1.0
  max_true_peak_dbtp: -1.0

file:
  containers: [mp4]
  max_size_bytes: 524288000     # 500 MB
  max_duration_seconds: 720     # 12 minutes
  portable_names: true
  ascii_only: true

consistency:
  uniform_resolution: true
  uniform_sample_rate: true
  uniform_frame_rate: true
  uniform_codec: true
  max_loudness_spread_lu: 1.5
  numbering_gaps: true
  chapter_ordering: true
```

Then, in the delivery script:

```sh
coursekit doctor ./delivery --profile client-acme --loudness \
  --md -o delivery-report.md || exit 1
```

Exit code `2` stops the script, and the client gets the Markdown report.

## A note on justifying limits

When you add a limit, write a comment saying where the number came from. The
shipped profiles do this, including where the number is a judgement rather than
a platform rule — the `udemy` profile says outright that Udemy publishes no
loudness figure and that −16 LUFS is convention.

A limit nobody can justify is a limit nobody can tune, and the first time it
fires on a file that is actually fine, someone will disable the whole check.
