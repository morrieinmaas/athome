package machines

import (
	"context"
	"fmt"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/steps"
)

// boxKeyCheck: the box's key is on the account as both kinds and logs in
// from the box. The login check also rewrites the box's pinned GitHub hosts.
func (a *addRun) boxKeyCheck(ctx context.Context, e *steps.Env) (bool, error) {
	res, err := a.r.Exec(ctx, a.m.Name, readPubScript, "", a.m.Hostname)
	if err != nil {
		return false, err
	}
	if err := reachErr(a.m.Name, res); err != nil {
		return false, err
	}
	if res.Code != 0 {
		return false, nil
	}
	auth, sign, err := a.gh.registered(ctx, keys.PubBlob(strings.TrimSpace(res.Stdout)))
	if err != nil || !auth || !sign {
		return false, err
	}
	// A failing login is for Apply to report.
	return a.box().CheckGitHub(ctx, false) == nil, nil
}

func (a *addRun) boxKeyApply(ctx context.Context, e *steps.Env) error {
	m := a.m
	res, err := a.r.Exec(ctx, m.Name, boxKeyScript, "", m.Hostname, m.Name)
	if err := okOrErr("create the box key", m.Name, res, err); err != nil {
		return err
	}
	pub := strings.TrimSpace(res.Stdout)
	if err := checkPub(pub); err != nil {
		return fmt.Errorf("box key of %s: %w", m.Name, err)
	}
	if err := a.gh.register(ctx, m.Name, pub); err != nil {
		return err
	}
	return a.box().CheckGitHub(ctx, false)
}

func (a *addRun) ghLoginCheck(ctx context.Context, e *steps.Env) (bool, error) {
	res, err := a.r.Exec(ctx, a.m.Name, ghStatusScript, "")
	if err != nil {
		return false, err
	}
	if err := reachErr(a.m.Name, res); err != nil {
		return false, err
	}
	return res.Code == 0, nil
}

func (a *addRun) ghLoginApply(ctx context.Context, e *steps.Env) error {
	res, err := a.r.Interactive(ctx, a.m.Name, ghLoginScript)
	if err != nil {
		return fmt.Errorf("gh auth login on %s: %w", a.m.Name, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("gh auth login on %s exited %d", a.m.Name, res.Code)
	}
	return nil
}
