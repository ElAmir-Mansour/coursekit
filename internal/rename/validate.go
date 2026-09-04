package rename

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Severity ranks a validation problem.
type Severity int

const (
	// Warn is worth reading before committing.
	Warn Severity = iota
	// Fatal means the plan must not be applied.
	Fatal
)

// Problem is something wrong with a plan.
type Problem struct {
	Severity Severity
	Op       *Op
	Message  string
}

func (p Problem) String() string {
	if p.Op != nil {
		return fmt.Sprintf("%s: %s", filepath.Base(p.Op.From), p.Message)
	}
	return p.Message
}

// maxNameBytes is the per-component limit on every filesystem coursekit
// targets.
const maxNameBytes = 255

// Validate checks a plan against the filesystem before anything is touched.
//
// The interesting case is a rename that only changes letter case. APFS and
// NTFS are case-insensitive by default, so "chap 1" and "Chap 1" are the same
// directory entry: a naive existence check reads that as a collision and
// refuses a rename that is in fact perfectly safe. os.SameFile settles it by
// identity rather than by name.
func Validate(p *Plan) []Problem {
	var problems []Problem

	add := func(sev Severity, op *Op, format string, args ...any) {
		problems = append(problems, Problem{Severity: sev, Op: op, Message: fmt.Sprintf(format, args...)})
	}

	// Exact target collisions, and targets that differ only in case.
	exact := map[string][]*Op{}
	folded := map[string][]*Op{}

	// Every source is stat'ed up front. A target that already exists is only
	// a collision if it is not itself one of the sources: in a swap or a
	// rotation, the entry sitting in the way is moved out of it by the
	// two-phase apply. Identity is compared with os.SameFile rather than by
	// path, so this holds on case-insensitive filesystems too.
	sourceInfos := make([]os.FileInfo, len(p.Ops))
	for i := range p.Ops {
		if fi, err := os.Lstat(p.Ops[i].From); err == nil {
			sourceInfos[i] = fi
		}
	}
	isSource := func(info os.FileInfo) bool {
		for _, si := range sourceInfos {
			if si != nil && os.SameFile(si, info) {
				return true
			}
		}
		return false
	}

	for i := range p.Ops {
		op := &p.Ops[i]

		fromInfo := sourceInfos[i]
		if fromInfo == nil {
			add(Fatal, op, "source no longer exists: %s", op.From)
			continue
		}

		newBase := filepath.Base(op.To)
		if newBase == "" || newBase == "." || newBase == string(filepath.Separator) {
			add(Fatal, op, "new name is empty")
			continue
		}
		if strings.ContainsAny(newBase, `/\`) {
			add(Fatal, op, "new name %q contains a path separator", newBase)
			continue
		}
		if len(newBase) > maxNameBytes {
			add(Fatal, op, "new name is %d bytes, over the %d-byte limit", len(newBase), maxNameBytes)
			continue
		}

		// Renames stay inside their own parent. Moving entries between
		// directories is a different operation with different risks, and this
		// tool deliberately does not do it.
		if filepath.Dir(op.From) != filepath.Dir(op.To) {
			add(Fatal, op, "would move between directories, which rename does not do")
			continue
		}

		if op.IsDir != fromInfo.IsDir() {
			add(Fatal, op, "plan disagrees with disk about whether this is a directory")
			continue
		}

		if toInfo, err := os.Lstat(op.To); err == nil {
			switch {
			case os.SameFile(fromInfo, toInfo):
				// Same entry under a different spelling: a case-only rename on
				// a case-insensitive filesystem. Allowed, and handled by the
				// two-phase apply.
			case isSource(toInfo):
				// Occupied by something this plan also moves, so the conflict
				// resolves itself during the temporary-name phase.
			default:
				add(Fatal, op, "target already exists: %s", filepath.Base(op.To))
				continue
			}
		}

		exact[op.To] = append(exact[op.To], op)
		folded[strings.ToLower(op.To)] = append(folded[strings.ToLower(op.To)], op)
	}

	for target, ops := range exact {
		if len(ops) > 1 {
			names := make([]string, len(ops))
			for i, o := range ops {
				names[i] = filepath.Base(o.From)
			}
			add(Fatal, nil, "%d entries would all be renamed to %q: %s",
				len(ops), filepath.Base(target), strings.Join(names, ", "))
		}
	}

	for target, ops := range folded {
		if len(ops) > 1 && len(exact[ops[0].To]) == len(ops) {
			// Already reported as an exact collision.
			continue
		}
		if len(ops) > 1 {
			names := make([]string, len(ops))
			for i, o := range ops {
				names[i] = filepath.Base(o.To)
			}
			add(Fatal, nil, "targets differ only in letter case, which collide on macOS and Windows: %s (%s)",
				strings.Join(names, ", "), filepath.Base(target))
		}
	}

	// Ordering guarantee the apply step depends on: a directory rename must
	// come after any rename of something inside it.
	for i := range p.Ops {
		if !p.Ops[i].IsDir {
			continue
		}
		prefix := p.Ops[i].From + string(filepath.Separator)
		for j := range p.Ops {
			if i == j {
				continue
			}
			if strings.HasPrefix(p.Ops[j].From, prefix) && j > i {
				add(Fatal, &p.Ops[i],
					"ordering error: %q is renamed before its contents", filepath.Base(p.Ops[i].From))
			}
		}
	}

	return problems
}

// Fatal reports whether any problem blocks the plan.
func HasFatal(problems []Problem) bool {
	for _, p := range problems {
		if p.Severity == Fatal {
			return true
		}
	}
	return false
}
