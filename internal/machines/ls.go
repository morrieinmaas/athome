package machines

import (
	"fmt"
	"io"
)

// List prints one line per machine in name order.
func List(home string, w io.Writer) error {
	ms, err := Load(home)
	if err != nil {
		return err
	}
	if len(ms) == 0 {
		_, err := fmt.Fprintln(w, "no machines")
		return err
	}
	for _, m := range ms {
		at := m.User + "@" + m.Target
		if m.Port != 0 && m.Port != 22 {
			at += fmt.Sprintf(":%d", m.Port)
		}
		if _, err := fmt.Fprintf(w, "%-16s %-20s %-28s %s\n", m.Name, m.Scope, at, fmt.Sprintf("%d repos", len(m.Repos))); err != nil {
			return err
		}
	}
	return nil
}
