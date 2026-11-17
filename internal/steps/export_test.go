package steps

import "time"

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

func SetNanobrewPaths(link, prefix string) (restore func()) {
	oldLink, oldPrefix := nanobrewLink, nanobrewPrefix
	nanobrewLink, nanobrewPrefix = link, prefix
	return func() { nanobrewLink, nanobrewPrefix = oldLink, oldPrefix }
}

func SetSudoInterval(d time.Duration) (restore func()) {
	old := sudoInterval
	sudoInterval = d
	return func() { sudoInterval = old }
}

// ReexecEnviron is the environment execBinary hands the new binary.
var ReexecEnviron = reexecEnviron
