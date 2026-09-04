package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/scan"
)

// Version is set at build time with -ldflags. It stays "dev" for a plain
// `go build`, and the module's own build info fills in the gaps.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version and dependency information",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			version, commit := versionInfo()

			fmt.Printf("coursekit %s\n", version)
			if commit != "" {
				fmt.Printf("  commit  %s\n", commit)
			}
			fmt.Printf("  go      %s\n", runtime.Version())
			fmt.Printf("  target  %s/%s\n", runtime.GOOS, runtime.GOARCH)

			// ffmpeg is optional, so report what is actually usable rather
			// than letting a missing binary surprise someone mid-command.
			if path := scan.FFprobePath(); path != "" {
				fmt.Printf("  ffprobe %s\n", path)
			} else {
				fmt.Printf("  ffprobe not found (scan works; doctor needs it)\n")
			}
			if err := scan.FFmpegAvailable(); err == nil {
				fmt.Printf("  ffmpeg  available (loudness measurement enabled)\n")
			} else {
				fmt.Printf("  ffmpeg  not found (--loudness unavailable)\n")
			}

			if dir, err := scan.CacheDir(); err == nil {
				fmt.Printf("  cache   %s\n", dir)
			}
		},
	}
}

func versionInfo() (version, commit string) {
	version = Version

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, ""
	}
	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 12 {
				commit = commit[:12]
			}
		case "vcs.modified":
			if s.Value == "true" && commit != "" {
				commit += " (modified)"
			}
		}
	}
	return version, commit
}
