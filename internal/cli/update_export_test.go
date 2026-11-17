package cli

// SetUpdateExec makes athome update call f instead of exec'ing the new binary.
func SetUpdateExec(f func(path string, argv []string) error) (restore func()) {
	old := updateExec
	updateExec = f
	return func() { updateExec = old }
}

// SetStdinTTY fakes whether stdin is a terminal.
func SetStdinTTY(tty bool) (restore func()) {
	old := stdinIsTTY
	stdinIsTTY = func() bool { return tty }
	return func() { stdinIsTTY = old }
}
