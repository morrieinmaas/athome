package steps

import "context"

// remoteSkips is the one remote-machine gate: `athome machine add` made the
// box's key and registered it with GitHub from the laptop, so init on the box
// itself never restores from Bitwarden, uploads a key or touches rbw or gh keys.
func remoteSkips(env *Env) bool { return env.Cfg.Machine == "remote" }

// skipOnRemote marks the step done on a remote box; apply it to every step that
// uploads to GitHub or touches rbw/Bitwarden.
func skipOnRemote(s Step) Step {
	check := s.Check
	s.Check = func(ctx context.Context, env *Env) (bool, error) {
		if remoteSkips(env) {
			return true, nil
		}
		return check(ctx, env)
	}
	return s
}

func skipAllOnRemote(list []Step) []Step {
	for i, s := range list {
		list[i] = skipOnRemote(s)
	}
	return list
}
