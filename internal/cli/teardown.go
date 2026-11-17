package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/morrieinmaas/athome/internal/teardown"
)

func runTeardown(args []string, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome teardown: %v\n", err)
		return exitError
	}
	host, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(stderr, "athome teardown: %v\n", err)
		return exitError
	}
	f, isFile := stdout.(*os.File)
	return teardown.Main(context.Background(), args, teardown.Env{
		Home: home, OS: runtime.GOOS, Hostname: host, Run: newRunner(),
		Stdin: os.Stdin, Stdout: stdout, Stderr: stderr,
		Color: isFile && isTerminal(f),
	})
}
