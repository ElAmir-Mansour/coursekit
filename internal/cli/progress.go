package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ElAmir-Mansour/coursekit/internal/export"
	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// progressPrinter shows scan progress on stderr, and only when stderr is a
// terminal. Progress written into a pipe would corrupt redirected output, and
// a carriage return in a log file is just noise.
type progressPrinter struct {
	enabled bool
	label   string

	mu      sync.Mutex
	lastLen int
	lastAt  time.Time
}

func newProgress(label string) *progressPrinter {
	return &progressPrinter{
		enabled: export.IsTerminal(os.Stderr) && !global.noColor,
		label:   label,
	}
}

// update renders one progress tick, throttled so a fast scan does not spend
// more time drawing than working.
func (p *progressPrinter) update(pr scan.Progress) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	final := pr.Done == pr.Total
	if !final && now.Sub(p.lastAt) < 60*time.Millisecond {
		return
	}
	p.lastAt = now

	label := p.label
	if pr.Phase == "loudness" {
		label = "measuring loudness"
	}

	line := fmt.Sprintf("  %s %d/%d  %s", label, pr.Done, pr.Total,
		truncateForWidth(pr.Current, 40))

	pad := ""
	if n := p.lastLen - len(line); n > 0 {
		pad = spaces(n)
	}
	fmt.Fprintf(os.Stderr, "\r%s%s", line, pad)
	p.lastLen = len(line)
}

// done clears the progress line so it does not linger above real output.
func (p *progressPrinter) done() {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastLen > 0 {
		fmt.Fprintf(os.Stderr, "\r%s\r", spaces(p.lastLen))
		p.lastLen = 0
	}
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func truncateForWidth(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return ""
	}
	return s[:n-1] + "…"
}
