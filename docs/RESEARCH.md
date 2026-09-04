# Research and measurements

Every performance choice in `coursekit` came from a measurement rather than an
assumption, and several of those measurements contradicted the plan. This is the
raw record.

All figures were taken on an Apple Silicon Mac (8 cores, 4 of them performance
cores), macOS 25.5, ffmpeg 8.0.1, Go 1.26.2, against a real 34-file course
totalling 5h 28m and 4.4 GiB of HEVC screen recordings.

---

## 1. Reading duration: three approaches

`scan` needs only duration and size per file. Three ways to get it, timed over
the same 34 files:

| Approach | Time | Notes |
|---|---|---|
| **`mvhd` + `tkhd` extraction, in-process** | **7.2 ms** | chosen |
| `mvhd` only, in-process | 3.4 ms | no dimensions |
| `ffprobe` subprocess per file, sequential | 2,000 ms | ~280× slower |
| `go-mp4`'s own `Probe()` | 10,285 ms | rejected |

`go-mp4`'s full `Probe()` was the surprise: it is **five times slower than
ffprobe**, because it walks the sample tables (`stco`, `stsz`) to build a
complete picture. It also returned `codec=0` and `avc=nil` for the HEVC tracks,
so it would not have given usable codec information either.

Correctness was confirmed against ffprobe on every file. Example:

```
mvhd duration=1133367 timescale=1000  ->  18m53.367s
tkhd track=1  1920x1200  matrix=identity
ffprobe                     1133.367  1920x1200
```

The track header carries *display* dimensions, and the transform matrix has to
be honoured or a portrait recording is reported as landscape. Results from the
fast path are marked as not-full, so codec and audio rules never run on them.

**Conclusion:** extract two named boxes, never walk the file.

## 2. Loudness: the video decode nobody needs

Measuring EBU R128 loudness on one 18m53s HEVC file:

| Command | Time | CPU |
|---|---|---|
| `ebur128`, video stream also decoded | 24.2 s | 526% |
| **`ebur128` with `-vn`** | **5.2 s** | 123% |
| `loudnorm=print_format=json` with `-vn` | 34.5 s | 104% |

Two findings:

- **`-vn` is a 4.6× win.** Without it ffmpeg saturates five cores decoding HEVC
  frames that are thrown away. This was the single largest optimisation in the
  project, and it was found only by measuring the same task two ways.
- **`loudnorm`'s JSON output costs 6.6×.** It is far more convenient to parse
  than `ebur128`'s indented plain text, but it performs a full two-pass
  analysis. The parser was worth writing.

The summary is logged at ffmpeg's *info* level, so the log level cannot be
lowered to `error` even though nothing else in the output is wanted.

### Worker count

Over 8 real course files, `-vn` enabled:

| Workers | Wall time |
|---|---|
| 1 | 3.32 s |
| 2 | 1.92 s |
| **4** | **1.57 s** |
| 8 | 1.59 s |

Throughput plateaus at 4 — the filter is largely single-threaded per file, so
beyond the machine's performance-core count there is only contention. The
original plan said 2 workers, based on the 526% CPU figure from the run *without*
`-vn`; once video decoding was removed, the correct answer changed.

**Whole course:** 5h 28m of audio measured in **26.5 seconds** at 538% CPU. The
plan had estimated 3.5 minutes.

## 3. What the checks found in a real course

A normal, competently produced course, scanned before publication. Its author
knew about none of these.

| Severity | Finding | Files |
|---|---|---|
| error | Aspect ratio 16:10, not the 16:9 Udemy requires | 34 |
| error | Loudness below the −14 LUFS YouTube target | 34 |
| warn | Audio bitrate ~159 kbps against a 256 kbps floor | 34 |
| warn | Loudness varies by 12.4 LU across the course | — |
| warn | Names unportable to Windows or a URL (`?`, `:`, trailing space) | 5 |
| warn | Video bitrate under 1000 kbps | 4 |
| warn | Course mixes two resolutions (1920×1200 and 1728×1080) | 2 |
| warn | Course mixes two sample rates (48 kHz and 44.1 kHz) | 2 |
| warn | Chapter folder spelled `Chpater`, so it sorts after chapter 9 | 1 |
| warn | Chapter folder ends in a space, invisible in Finder | 1 |
| note | Two chapter folders carry no number at all | 2 |

Loudness distribution, quietest first: −33.0, −32.7, −29.2, −28.7, −28.6, −28.2,
−27.9, −26.4 … −21.9, −21.9, **−20.6** LUFS.

Not one file reached −20 LUFS. The two resolution outliers were also the two
sample-rate outliers — both re-exports made with different settings.

The spread is the interesting part. A creator listens to each lesson
immediately after recording it, at whatever volume their Mac is at, so a course
that drifts 12 LU between chapters sounds fine throughout production and wrong
to a student.

## 4. Platform requirements

Sources for the shipped profiles.

### Udemy

[Video Standards](https://business-support.udemy.com/hc/en-us/articles/360022570274-Video-Standards) ·
[bitrate discussion](https://community.udemy.com/t5/Audio-and-video-solutions/Video-Bitrate-Requirements-for-Courses/m-p/75164)

- H.264, HEVC or ProRes; 1920×1080 or better, 1280×720 minimum
- **16:9, landscape**; 25–60 fps
- H.264 1080p ≈10 Mb/s, 720p ≈5 Mb/s; HEVC 1080p ≈6 Mb/s, 720p ≈3 Mb/s
- AAC at 256 kb/s or better, or PCM; 2 channels
- MP4 or MOV; ≤4 GB and ≤4 hours per video

Udemy publishes **no loudness figure**. The profile targets −16 LUFS with a 3 LU
tolerance, which is the widely used figure for spoken educational content, and
the YAML says as much so nobody mistakes it for a platform rule.

The minimum video bitrate is set at 1000 kbps, well under Udemy's
recommendation. Screen recordings of static slides compress far better than
camera footage, and flagging every one of them would be noise.

### YouTube

Playback is normalised toward roughly **−14 LUFS**
([LUFS](https://en.wikipedia.org/wiki/LUFS)). Aspect ratio is unconstrained —
YouTube pillarboxes whatever it is given — so the profile leaves shape alone and
checks only consistency. Quiet audio is not rejected; it simply plays quieter
than everything around it, which is why the loudness tolerance is the tightest
part of this profile.

### Self-hosted / LMS

The binding constraint is browser support, not a platform rule.
[HEVC video support](https://developer.mozilla.org/en-US/docs/Web/Media/Formats/Video_codecs)
is good in Safari and unreliable in Chrome and Firefox, so H.264 is the only
codec that plays everywhere without a fallback.

This is the rule that catches an otherwise perfect Mac screen recording:
QuickTime and most Mac capture tools default to HEVC, which is exactly wrong for
a course served from your own site — and it fails for students with no error
message, just a black frame.

## 5. Prior art

Existing answers to "how long is this folder of video", and why they were not
enough:

- The [`find | ffprobe | paste | bc` one-liner](https://www.arj.no/2023/08/02/ffprobe-folders/) —
  the baseline. Correct, slow, and answers only the one question.
- [Sum Media Duration](https://github.com/edgarh92/Sum-Media-Duration) — a
  Python tool doing exactly this, one subprocess per file.
- `mediainfo --Inform` in a shell loop — same shape, different binary.

All of them total durations. None of them order chapters, notice that a course
mixes sample rates, or tell you your videos are the wrong aspect ratio for the
platform you are about to upload to.

## 6. Things measured and then discarded

- **`go-mp4`'s `Probe()`** — §1. Slower than the subprocess it was meant to
  replace.
- **`loudnorm` JSON** — §2. 6.6× the cost of parsing plain text.
- **8 loudness workers** — §2. No faster than 4.
- **Reading dimensions from `stsd`/`VisualSampleEntry`** rather than `tkhd`.
  It matches what ffprobe reports more precisely, but needs a type switch across
  every codec's sample entry (`avc1`, `hvc1`, `hev1`, `mp4v`, …). `tkhd` plus
  the rotation matrix gave identical answers on every test file for a fraction
  of the code, and fast-path results are marked not-full anyway.
