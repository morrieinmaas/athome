package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/teardown"
)

func runTeardown(args []string, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome teardown: %v\n", err)
		return exitError
	}
	r := newRunner()
	host := steps.KeyHostname(context.Background(), r)
	f, isFile := stdout.(*os.File)
	return teardown.Main(context.Background(), args, teardown.Env{
		Home: home, OS: runtime.GOOS, Hostname: host, Run: r,
		Stdin: os.Stdin, Stdout: stdout, Stderr: stderr,
		Color: isFile && isTerminal(f),
	})
}
