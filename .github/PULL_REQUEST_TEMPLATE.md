## What this changes

<!-- One or two sentences. If it fixes an issue, say "Fixes #123". -->

## Why

<!-- The reasoning, if it is not obvious from the change itself. Measurements
     are very welcome: this project has a habit of them. -->

## Checks

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` is clean
- [ ] `go test -race ./...` passes
- [ ] ffmpeg was installed while testing (the fixture tests skip without it,
      so a run without ffmpeg passes while testing much less than it appears)

## If this touches the rename engine

That is the only part of coursekit that modifies files, so it has the highest
bar. Confirm these still hold:

- [ ] apply-then-undo restores the tree exactly, trailing space and typo included
- [ ] a swap (`A→B`, `B→A`) works in both directions
- [ ] a case-only rename works on a case-insensitive filesystem
- [ ] a failure part-way through rolls back fully and leaves no journal

## If this adds a lint rule or profile limit

- [ ] there is a comment saying where the number came from

A limit nobody can justify is a limit nobody can tune.
