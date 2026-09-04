// Command coursekit inspects, checks and tidies folders of course recordings.
package main

import (
	"os"

	"github.com/ElAmir-Mansour/coursekit/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
