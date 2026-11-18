package status

// MachinesStatus is the remote inventory. It never affects Report.Current.
type MachinesStatus struct {
	Count    int      `json:"count"`
	Warnings []string `json:"warnings"`
}

// MachinesFrom builds the status from a loaded inventory: its size, the local
// scope warnings and the load error. A broken inventory is a warning, not an
// error. The caller loads it because internal/machines imports this package
// through internal/steps.
func MachinesFrom(count int, warnings []string, loadErr error) MachinesStatus {
	s := MachinesStatus{Count: count, Warnings: []string{}}
	if loadErr != nil {
		s.Count = 0
		s.Warnings = append(s.Warnings, loadErr.Error())
		return s
	}
	s.Warnings = append(s.Warnings, warnings...)
	return s
}
