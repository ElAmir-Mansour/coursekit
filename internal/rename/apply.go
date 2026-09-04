package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tempPrefix marks the intermediate names used during a two-phase rename.
const tempPrefix = ".coursekit-tmp-"

// step is one filesystem rename that actually happened, recorded so it can be
// undone during a rollback.
type step struct{ from, to string }

// Apply performs a validated plan and records how to reverse it.
func Apply(p *Plan) (*Journal, error) {
	if p.Empty() {
		return nil, fmt.Errorf("plan is empty")
	}
	if problems := Validate(p); HasFatal(problems) {
		return nil, fmt.Errorf("plan is not safe to apply: %s", problems[0].String())
	}

	// The journal is written before anything moves. If the process is killed
	// half-way, the pending record is what makes that discoverable rather
	// than invisible.
	journal, err := newJournal(p)
	if err != nil {
		return nil, fmt.Errorf("could not write the undo journal, refusing to rename: %w", err)
	}

	done, err := runOps(p.Ops)
	if err != nil {
		rollback(done)
		journal.discard()
		return nil, err
	}

	if err := journal.commit(p.Ops); err != nil {
		// The renames worked but the record of them did not, which would
		// leave no way back. Undo the work rather than strand the user.
		rollback(done)
		journal.discard()
		return nil, fmt.Errorf("renames were rolled back because the undo journal could not be finalised: %w", err)
	}

	return journal, nil
}

// Undo reverses an applied journal.
//
// This runs the inverse operations through the very same engine as Apply,
// rather than reimplementing rename logic in the other direction. That matters
// for swaps: undoing "A to B, B to A" needs the temporary-name phase exactly
// as much as applying it did, and a simpler per-file loop silently refuses
// because each target is occupied by the other file.
//
// The ops are inverted in reverse order, so a chapter folder is restored
// before the lessons inside it. Those lesson paths were recorded while the
// folder still had its original name, and only resolve again once it does.
func Undo(j *Journal) (reverted int, err error) {
	inverse := make([]Op, 0, len(j.Ops))
	for i := len(j.Ops) - 1; i >= 0; i-- {
		op := j.Ops[i]
		inverse = append(inverse, Op{From: op.To, To: op.From, IsDir: op.IsDir})
	}

	var (
		done    []step
		skipped []string
	)

	// Existence is checked one group at a time rather than all at once,
	// because a lesson's recorded path only becomes valid again after its
	// chapter folder has been restored, and that happens in an earlier group.
	for _, group := range groupAdjacentByParent(inverse) {
		var live []Op
		for _, op := range group {
			if _, statErr := os.Lstat(op.From); statErr != nil {
				skipped = append(skipped, fmt.Sprintf("%s is no longer there", filepath.Base(op.From)))
				continue
			}
			live = append(live, op)
		}
		if len(live) == 0 {
			continue
		}

		groupDone, runErr := runOps(live)
		done = append(done, groupDone...)
		if runErr != nil {
			rollback(done)
			return 0, runErr
		}
		reverted += len(live)
	}

	if reverted == 0 {
		if len(skipped) > 0 {
			return 0, fmt.Errorf("nothing to undo: %s", strings.Join(skipped, "; "))
		}
		return 0, fmt.Errorf("nothing to undo")
	}

	// Mark the journal so a second undo does not try to reverse it again.
	if markErr := j.markUndone(); markErr != nil {
		return reverted, fmt.Errorf("undo succeeded but the journal could not be marked: %w", markErr)
	}

	if len(skipped) > 0 {
		return reverted, fmt.Errorf("undo finished with %d skipped entr(ies): %s",
			len(skipped), strings.Join(skipped, "; "))
	}
	return reverted, nil
}

// runOps performs renames in two phases per directory.
//
// A single-phase loop cannot express a swap: renaming A to B while B still
// exists either fails or destroys B. It cannot express a case-only rename on a
// case-insensitive filesystem either, since source and target are the same
// directory entry. Parking everything under a unique temporary name first
// removes both problems.
//
// The caller's ordering is preserved; only consecutive ops sharing a parent
// are grouped. Re-sorting here would break the guarantee that contents are
// renamed before the folder holding them.
func runOps(ops []Op) (done []step, err error) {
	mv := func(from, to string) error {
		if renameErr := os.Rename(from, to); renameErr != nil {
			return renameErr
		}
		done = append(done, step{from: from, to: to})
		return nil
	}

	for _, group := range groupAdjacentByParent(ops) {
		temps := make([]string, len(group))

		// Phase one: park every entry under a name nothing else can want.
		for i, op := range group {
			tmp, tmpErr := freeTempPath(op.From)
			if tmpErr != nil {
				return done, tmpErr
			}
			if renameErr := mv(op.From, tmp); renameErr != nil {
				return done, fmt.Errorf("rename %s: %w", filepath.Base(op.From), renameErr)
			}
			temps[i] = tmp
		}

		// Phase two: move each one into place.
		for i, op := range group {
			if renameErr := mv(temps[i], op.To); renameErr != nil {
				return done, fmt.Errorf("rename %s to %s: %w",
					filepath.Base(op.From), filepath.Base(op.To), renameErr)
			}
		}
	}

	return done, nil
}

// rollback reverses completed renames, most recent first. It is best effort:
// if a reversal fails there is nothing more useful to try, and the journal
// still records what was intended.
func rollback(done []step) {
	for i := len(done) - 1; i >= 0; i-- {
		_ = os.Rename(done[i].to, done[i].from)
	}
}

// groupAdjacentByParent splits ops into runs of consecutive entries that share
// a parent directory, preserving the order it was given.
//
// Ops from one directory are always adjacent in a plan, because Build sorts by
// depth and then by path, and reversing that order for an undo keeps the runs
// contiguous too.
func groupAdjacentByParent(ops []Op) [][]Op {
	var (
		out     [][]Op
		current []Op
		parent  string
	)

	for _, op := range ops {
		p := filepath.Dir(op.From)
		if current != nil && p != parent {
			out = append(out, current)
			current = nil
		}
		parent = p
		current = append(current, op)
	}
	if current != nil {
		out = append(out, current)
	}
	return out
}

// freeTempPath returns an unused temporary path alongside the given one.
func freeTempPath(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	for i := 0; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s%d-%s", tempPrefix, i, base))
		if len(filepath.Base(candidate)) > maxNameBytes {
			// Keep the temporary name legal even for a very long original.
			candidate = filepath.Join(dir, fmt.Sprintf("%s%d", tempPrefix, i))
		}
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find a free temporary name in %s", dir)
}
