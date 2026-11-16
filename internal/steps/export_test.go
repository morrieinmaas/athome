package steps

// SetNanobrewBin points the darwin package-manager check at another nb path.
func SetNanobrewBin(p string) (restore func()) {
	old := nanobrewBin
	nanobrewBin = p
	return func() { nanobrewBin = old }
}
