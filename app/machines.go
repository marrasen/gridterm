package app

import (
	"slices"

	"github.com/marrasen/kakel/machines"
)

// used reports whether anything open here is on machine id or beyond
// it: a pane, a tunnel, a job, a file clipped or a copy listed. What
// the machine's own connection keeps, the registry knows.
func (a *app) used(id machines.ID) bool {
	on := func(m machines.ID) bool { return m.Of(id) }
	if c := a.clip; c != nil && on(c.machine) {
		return true
	}
	if slices.ContainsFunc(a.running, func(r *running) bool { return on(r.from) || on(r.to) }) {
		return true
	}
	return slices.ContainsFunc(a.st.Panes, func(p Pane) bool {
		return on(p.Machine) || p.On != "" && machines.FarID(p.Machine, p.On) == id
	}) ||
		slices.ContainsFunc(a.st.Tunnels, func(t Tunnel) bool { return on(t.Machine) }) ||
		slices.ContainsFunc(a.st.Jobs, func(j Job) bool { return on(j.Machine) })
}

// forgetUnused forgets the machines nothing is kept on any more: quick
// connections, removed servers still named, and what windows called
// machines beyond them. Their logs, and what they said about their
// paths, go too.
func (a *app) forgetUnused() {
	for _, id := range a.machines.Forget(a.used) {
		a.st.Accounts = slices.DeleteFunc(a.st.Accounts, func(n machines.ID) bool { return n == id })
		a.forgetFar(id)
	}
}
