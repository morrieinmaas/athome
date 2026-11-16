package config

import "errors"

var ErrUsage = errors.New("not implemented: T1")

type Options struct {
	ConfigPath string
	RepoRoot   string
	Home       string
	Env        map[string]string
	Machine    string
	NoAgents   bool
}

type Config struct {
	Machine       string
	IncludeAgents bool
	Data          map[string]string
	Source        string
}

func Load(o Options) (Config, error) {
	panic("not implemented: T1")
}
