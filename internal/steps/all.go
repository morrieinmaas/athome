package steps

// All is the full init list in the order the bash bootstrap ran them.
func All() []Step {
	var all []Step
	for _, g := range [][]Step{Prepare(), SSHKey(), GitHub(), Data(), Baseline(), Finish()} {
		all = append(all, g...)
	}
	return all
}
