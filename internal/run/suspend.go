package run

import "context"

// Suspending wraps r so that commands needing the terminal (Interactive,
// NeedsSudo or a sudo without -n) run inside around, e.g. with a TUI suspended. Others go straight to r.
func Suspending(r Runner, around func(func())) Runner {
	return suspending{r, around}
}

type suspending struct {
	Runner
	around func(func())
}

func (s suspending) Run(ctx context.Context, c Cmd) (res Result, err error) {
	if !c.Interactive && !c.NeedsSudo && !promptsForSudo(c) {
		return s.Runner.Run(ctx, c)
	}
	s.around(func() { res, err = s.Runner.Run(ctx, c) })
	return res, err
}

// promptsForSudo is a direct sudo command without -n, the same rule as the sudo warm-up.
func promptsForSudo(c Cmd) bool {
	return c.Name == "sudo" && (len(c.Args) == 0 || c.Args[0] != "-n")
}
