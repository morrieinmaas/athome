package run

import "context"

// Suspending wraps r so that commands needing the terminal (Interactive or
// NeedsSudo) run inside around, e.g. with a TUI suspended. Others go straight to r.
func Suspending(r Runner, around func(func())) Runner {
	return suspending{r, around}
}

type suspending struct {
	Runner
	around func(func())
}

func (s suspending) Run(ctx context.Context, c Cmd) (res Result, err error) {
	if !c.Interactive && !c.NeedsSudo {
		return s.Runner.Run(ctx, c)
	}
	s.around(func() { res, err = s.Runner.Run(ctx, c) })
	return res, err
}
