package run

import "context"

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
}

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
	LookPath(name string) (string, error)
}

func Exec() Runner {
	panic("not implemented: T2")
}

type Fake struct{}

func NewFake() *Fake {
	panic("not implemented: T2")
}

func (f *Fake) On(name string, args ...string) *Fake {
	panic("not implemented: T2")
}

func (f *Fake) Return(r Result) {
	panic("not implemented: T2")
}

func (f *Fake) Calls() []Cmd {
	panic("not implemented: T2")
}

func (f *Fake) Have(names ...string) {
	panic("not implemented: T2")
}

func (f *Fake) Run(ctx context.Context, c Cmd) (Result, error) {
	panic("not implemented: T2")
}

func (f *Fake) LookPath(name string) (string, error) {
	panic("not implemented: T2")
}
