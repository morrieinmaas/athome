package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
	// Interactive inherits os.Stdin, os.Stdout and os.Stderr, so the user sees
	// the command's output and can answer its prompts. Result then carries only Code.
	Interactive bool
	// NeedsSudo marks a command that runs sudo internally, so the sudo
	// warm-up wrapper caches credentials before it.
	NeedsSudo bool
}

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Runner is the only way steps reach external tools. A non-zero exit is a
// Result; the error is reserved for a process that cannot start.
type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
	LookPath(name string) (string, error)
}

type execRunner struct{}

func Exec() Runner { return execRunner{} }

func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (execRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	// A child of the command can hold the output pipes open past the deadline;
	// WaitDelay makes Run return anyway.
	cmd.WaitDelay = 2 * time.Second
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	if c.Interactive {
		if c.Stdin == "" {
			cmd.Stdin = os.Stdin
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	} else {
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
	}

	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		res.Code = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("run %s: %w", c.Name, err)
	}
	return res, nil
}
