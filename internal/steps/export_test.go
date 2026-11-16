package steps

// SetNanobrewBin points the darwin package-manager check at another nb path.
func SetNanobrewBin(p string) (restore func()) {
	old := nanobrewBin
	nanobrewBin = p
	return func() { nanobrewBin = old }
}

// SetHostnameSources swaps the os.Hostname call and the /etc/hostname path.
func SetHostnameSources(osHost func() (string, error), etcPath string) (restore func()) {
	oldOS, oldEtc := osHostname, etcHostname
	osHostname, etcHostname = osHost, etcPath
	return func() { osHostname, etcHostname = oldOS, oldEtc }
}
