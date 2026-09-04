# Security policy

## Reporting a vulnerability

Please report security issues through
**[GitHub's private vulnerability reporting](https://github.com/ElAmir-Mansour/coursekit/security/advisories/new)**
rather than opening a public issue.

That keeps the report private until there is a fix, and it does not require
publishing an email address that would be scraped.

I will acknowledge a report within a few days. If a fix is needed, the advisory
is published alongside the release that carries it.

## Supported versions

Only the latest release receives fixes. This is a young single-maintainer
project; backporting to older tags is not realistic.

## What is in scope

`coursekit` reads media files, writes a cache, and renames files you point it
at. The interesting attack surface is therefore:

- **Path handling.** A crafted filename or a symlink causing a write outside
  the course folder, or a rename escaping its own parent directory. Renames are
  validated to stay within their parent, and that check failing open would be a
  real bug.
- **Malformed media.** The MP4 reader parses container boxes directly. A
  crafted file causing a panic, an unbounded allocation, or a hang is worth
  reporting. It runs in-process, so it is the most sensitive parser here.
- **Cache and journal files.** These are read back and acted upon. A crafted
  journal causing a rename to an unexpected path would be serious.
- **Command construction.** `ffmpeg` and `ffprobe` are executed with file
  paths as arguments. They are passed as an argument slice, never through a
  shell, so a filename cannot inject a command — but a way around that is
  absolutely worth reporting.

## What is not in scope

- `ffmpeg` and `ffprobe` themselves. Report those
  [upstream](https://ffmpeg.org/security.html).
- The `ffmpeg` commands `doctor` prints as suggested fixes. They are printed
  for you to read and run yourself; `coursekit` never executes them.
- Anything requiring an attacker to already be able to run code as you.
