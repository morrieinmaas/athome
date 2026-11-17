package status

import (
	"os"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
)

// RotateEvery is how old the laptop key may get before status reminds you.
const RotateEvery = 90 * 24 * time.Hour

// KeysStatus is the rotation reminder. It never affects Report.Current.
type KeysStatus struct {
	LastRotated string `json:"last_rotated"`
	Due         bool   `json:"due"`
}

// Keys dates the last rotation from rotate.json, else the laptop key's mtime.
// With no laptop key there is nothing to remind about.
func Keys(home, host string, now time.Time) KeysStatus {
	fi, err := os.Stat(keys.LaptopKey(home, host))
	if err != nil {
		return KeysStatus{}
	}
	t, ok := keys.LastRotated(home)
	if !ok {
		t = fi.ModTime()
	}
	return KeysStatus{LastRotated: t.UTC().Format(time.RFC3339), Due: now.Sub(t) > RotateEvery}
}

// DaysSince is the whole days between the reminder's date and now.
func (k KeysStatus) DaysSince(now time.Time) int {
	t, err := time.Parse(time.RFC3339, k.LastRotated)
	if err != nil {
		return 0
	}
	return int(now.Sub(t).Hours() / 24)
}
