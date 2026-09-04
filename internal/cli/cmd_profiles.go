package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ElAmir-Mansour/coursekit/internal/lint"
)

func newProfilesCmd() *cobra.Command {
	var show string

	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "List the available lint profiles",
		Long: `profiles lists the rule sets that 'doctor' can check against.

Built-in profiles are compiled into the binary. To write your own, copy one out
with --show and save it in your profile directory; a file there overrides a
built-in of the same name.`,
		Example: `  coursekit profiles
  coursekit profiles --show udemy > ~/.config/coursekit/profiles/mine.yaml`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pal := paletteFor(os.Stdout)

			if show != "" {
				p, err := lint.LoadProfile(show)
				if err != nil {
					return err
				}
				// Print the source rather than a re-marshalled struct, so the
				// comments explaining each rule survive.
				data, err := lint.ProfileSource(show)
				if err != nil {
					return fmt.Errorf("profile %q is loadable but its source could not be read: %w", p.Name, err)
				}
				fmt.Print(string(data))
				return nil
			}

			fmt.Printf("  %sBuilt-in profiles%s\n\n", pal.Bold, pal.Reset)
			for _, name := range lint.BuiltinNames() {
				p, err := lint.LoadProfile(name)
				if err != nil {
					continue
				}
				fmt.Printf("    %s%-9s%s %s\n", pal.Cyan, p.Name, pal.Reset, p.Description)
			}

			dir := lint.UserProfileDir()
			fmt.Printf("\n  %sYour profiles%s  %s%s%s\n", pal.Bold, pal.Reset, pal.Dim, dir, pal.Reset)

			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) == 0 {
				fmt.Printf("    %s(none yet)%s\n", pal.Dim, pal.Reset)
			} else {
				for _, e := range entries {
					name := e.Name()
					if e.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml")) {
						continue
					}
					fmt.Printf("    %s%s%s\n", pal.Cyan,
						strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"), pal.Reset)
				}
			}

			fmt.Printf("\n  %sStart your own from a built-in:%s\n", pal.Dim, pal.Reset)
			fmt.Printf("    mkdir -p %s\n", dir)
			fmt.Printf("    coursekit profiles --show lms > %s\n", filepath.Join(dir, "mine.yaml"))
			fmt.Printf("    coursekit doctor . --profile mine\n")

			return nil
		},
	}

	cmd.Flags().StringVar(&show, "show", "", "print a profile's YAML source")
	return cmd
}
