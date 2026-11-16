package steps

// All is the full init list in bash order (bootstrap.sh).
func All() []Step {
	var all []Step
	for _, g := range [][]Step{Prepare(), SSHKey(), GitHub(), Data(), Baseline(), Finish()} {
		all = append(all, g...)
	}
	return all
}
