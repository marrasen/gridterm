package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/marrasen/gunim"
)

// The program side of the window. It owns the panes, how they are
// arranged, and which one has the keyboard, on a goroutine of its own.
// It hears what the window asks for as intents and, after each change,
// publishes the state the window shows.

// State is what the window shows.
type State struct {
	// Panes are the open panes, in the sidebar's order.
	Panes []Pane
	// Stage is the arrangement on screen: the group of panes the
	// focused pane belongs to. Nil when no pane is open.
	Stage *Box
	Focus string
	// Sidebar is whether the sidebar shows, and SidebarWidth how wide.
	Sidebar      bool
	SidebarWidth float32
	Status       string
	// Output counts the times shells wrote, so the window copies their
	// screens once a frame however often they write.
	Output uint64
}

// Pane is one pane, as the sidebar lists it.
type Pane struct {
	ID    string
	Title string
}

// Box is one part of an arrangement: a pane, or a split of two boxes.
type Box struct {
	// Pane names the pane a leaf shows; a split has none.
	Pane string
	// ID names a split, so the window keeps its divider where it is
	// from one state to the next.
	ID       string
	Vertical bool
	// Share is the first box's share of the split's space.
	Share float32
	// Opening marks a split just made, whose new pane slides in.
	Opening bool
	A, B    *Box
}

func (b *Box) clone() *Box {
	if b == nil {
		return nil
	}
	c := *b
	c.A, c.B = b.A.clone(), b.B.clone()
	return &c
}

// leaves appends the panes under b, first to last.
func (b *Box) leaves(out []string) []string {
	switch {
	case b == nil:
		return out
	case b.Pane != "":
		return append(out, b.Pane)
	}
	return b.B.leaves(b.A.leaves(out))
}

// beside returns the pane nearest pane across the split that holds
// it: the first one on that side when pane is first, and the last one
// when it is second.
func (b *Box) beside(pane string) string {
	if b == nil || b.Pane != "" {
		return ""
	}
	switch {
	case b.A.Pane == pane:
		return b.B.leaves(nil)[0]
	case b.B.Pane == pane:
		l := b.A.leaves(nil)
		return l[len(l)-1]
	}
	if n := b.A.beside(pane); n != "" {
		return n
	}
	return b.B.beside(pane)
}

// replace returns b with the leaf for pane swapped for with, which may
// be nil to take the leaf out: its split then gives way to the other
// side.
func (b *Box) replace(pane string, with *Box) *Box {
	switch {
	case b == nil:
		return nil
	case b.Pane == pane:
		return with
	case b.Pane != "":
		return b
	}
	a, c := b.A.replace(pane, with), b.B.replace(pane, with)
	switch {
	case a == nil:
		return c
	case c == nil:
		return a
	}
	n := *b
	n.A, n.B = a, c
	return &n
}

// Intents: what the window asks the program for.
type (
	// NewTerminal opens a shell in a pane of its own.
	NewTerminal struct{}
	// SplitPane splits the focused pane, and opens a shell in the new
	// half: to the right, or below with Vertical.
	SplitPane struct{ Vertical bool }
	// ClosePane closes a pane, or the focused one when Pane is empty.
	ClosePane struct{ Pane string }
	// FocusPane gives a pane the keyboard, bringing its group on stage.
	FocusPane struct{ Pane string }
	// NextPane moves the keyboard to the next pane in the sidebar, or
	// the one before with Back.
	NextPane struct{ Back bool }
	// PopOut takes the focused pane out of its split, onto a stage of
	// its own.
	PopOut struct{}
	// ToggleSidebar shows or hides the sidebar.
	ToggleSidebar struct{}
	// SplitMoved says where the pointer left a split's divider.
	SplitMoved struct {
		Split string
		Share float32
	}
	// SidebarMoved says how wide the pointer left the sidebar.
	SidebarMoved struct{ Width float32 }
	// Exit closes the window, and every shell in it.
	Exit struct{}
)

// app is the program side's state. It belongs to the goroutine running
// run.
type app struct {
	c      gunim.Client
	shells *shells
	st     State
	// groups holds each group's arrangement, and groupOf each pane's
	// group.
	groups  map[int]*Box
	groupOf map[string]int
	next    int
	// wake hears that a shell wrote, and events carries changes from
	// the shells' goroutines to this one.
	wake   chan struct{}
	events chan func()
}

// shells holds the running shells by pane, for the window to draw.
type shells struct {
	mu sync.Mutex
	m  map[string]*shell
}

func (s *shells) get(id string) *shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

func (s *shells) set(id string, sh *shell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh == nil {
		delete(s.m, id)
		return
	}
	s.m[id] = sh
}

func newApp(c gunim.Client, sh *shells) *app {
	return &app{
		c:       c,
		shells:  sh,
		st:      State{Sidebar: true, SidebarWidth: 220},
		groups:  map[int]*Box{},
		groupOf: map[string]int{},
		wake:    make(chan struct{}, 1),
		events:  make(chan func(), 64),
	}
}

// run serves the window until it closes or ctx ends.
func (a *app) run(ctx context.Context) error {
	if err := a.openTerminal(); err != nil {
		return err
	}
	a.publish()
	intents := a.c.Intents()
	for {
		select {
		case <-ctx.Done():
			return nil
		case env, ok := <-intents:
			if !ok {
				return a.c.Err()
			}
			a.handle(env.Intent)
		case <-a.wake:
			a.st.Output++
		case f := <-a.events:
			f()
		}
		if len(a.st.Panes) == 0 {
			a.c.Close()
			intents = a.c.Intents()
			for range intents {
			}
			return a.c.Err()
		}
		a.publish()
	}
}

func (a *app) publish() {
	st := a.st
	st.Panes = slices.Clone(a.st.Panes)
	st.Stage = a.groups[a.groupOf[a.st.Focus]].clone()
	_ = a.c.Publish(windowTopic, st)
	// A split opens once; after that it is only a split.
	for _, g := range a.groups {
		clearOpening(g)
	}
}

func clearOpening(b *Box) {
	if b == nil {
		return
	}
	b.Opening = false
	clearOpening(b.A)
	clearOpening(b.B)
}

func (a *app) handle(in gunim.Intent) {
	var err error
	switch in := in.(type) {
	case NewTerminal:
		err = a.openTerminal()
	case SplitPane:
		err = a.split(in.Vertical)
	case ClosePane:
		id := in.Pane
		if id == "" {
			id = a.st.Focus
		}
		a.closePane(id)
	case FocusPane:
		if a.has(in.Pane) {
			a.st.Focus = in.Pane
		}
	case NextPane:
		a.nextPane(in.Back)
	case PopOut:
		a.popOut()
	case ToggleSidebar:
		a.st.Sidebar = !a.st.Sidebar
	case SplitMoved:
		setShare(a.groups[a.groupOf[a.st.Focus]], in.Split, in.Share)
	case SidebarMoved:
		a.st.SidebarWidth = in.Width
	case Exit:
		for len(a.st.Panes) > 0 {
			a.closePane(a.st.Panes[0].ID)
		}
	}
	if err != nil {
		a.st.Status = err.Error()
	}
}

func setShare(b *Box, id string, share float32) {
	if b == nil {
		return
	}
	if b.ID == id {
		b.Share = share
	}
	setShare(b.A, id, share)
	setShare(b.B, id, share)
}

func (a *app) has(id string) bool {
	return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
}

// start starts a shell for a new pane, and returns the pane.
func (a *app) start() (string, error) {
	a.next++
	id := "p" + strconv.Itoa(a.next)
	title := fmt.Sprintf("Terminal %d", a.next)
	sh, err := startShell(shellHooks{
		output: func() {
			select {
			case a.wake <- struct{}{}:
			default:
			}
		},
		title: func(t string) { a.events <- func() { a.retitle(id, t) } },
		exit:  func() { a.events <- func() { a.closePane(id) } },
	})
	if err != nil {
		return "", err
	}
	a.shells.set(id, sh)
	a.st.Panes = append(a.st.Panes, Pane{ID: id, Title: title})
	return id, nil
}

// openTerminal opens a shell on a stage of its own.
func (a *app) openTerminal() error {
	id, err := a.start()
	if err != nil {
		return err
	}
	a.next++
	g := a.next
	a.groups[g] = &Box{Pane: id}
	a.groupOf[id] = g
	a.st.Focus = id
	return nil
}

// split opens a shell beside the focused pane.
func (a *app) split(vertical bool) error {
	focus := a.st.Focus
	g, ok := a.groupOf[focus]
	if !ok {
		return a.openTerminal()
	}
	id, err := a.start()
	if err != nil {
		return err
	}
	a.next++
	box := &Box{
		ID: "s" + strconv.Itoa(a.next), Vertical: vertical, Share: 0.5, Opening: true,
		A: &Box{Pane: focus}, B: &Box{Pane: id},
	}
	a.groups[g] = a.groups[g].replace(focus, box)
	a.groupOf[id] = g
	a.st.Focus = id
	return nil
}

// take takes a pane out of its group's arrangement, and reports the
// pane that should have the keyboard in its place: the nearest pane on
// the other side of the split it leaves.
func (a *app) take(id string) string {
	g, ok := a.groupOf[id]
	if !ok {
		return ""
	}
	delete(a.groupOf, id)
	next := a.groups[g].beside(id)
	rest := a.groups[g].replace(id, nil)
	if rest == nil {
		delete(a.groups, g)
		return ""
	}
	a.groups[g] = rest
	return next
}

func (a *app) closePane(id string) {
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
	if i < 0 {
		return
	}
	if sh := a.shells.get(id); sh != nil {
		sh.close()
	}
	a.shells.set(id, nil)
	next := a.take(id)
	a.st.Panes = slices.Delete(a.st.Panes, i, i+1)
	if a.st.Focus == id {
		switch {
		case next != "":
			a.st.Focus = next
		case len(a.st.Panes) > 0:
			a.st.Focus = a.st.Panes[max(0, i-1)].ID
		default:
			a.st.Focus = ""
		}
	}
}

func (a *app) nextPane(back bool) {
	n := len(a.st.Panes)
	if n == 0 {
		return
	}
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == a.st.Focus })
	step := 1
	if back {
		step = n - 1
	}
	a.st.Focus = a.st.Panes[(i+step)%n].ID
}

// popOut moves the focused pane onto a stage of its own.
func (a *app) popOut() {
	id := a.st.Focus
	if b := a.groups[a.groupOf[id]]; b == nil || b.Pane == id {
		return
	}
	a.take(id)
	a.next++
	a.groups[a.next] = &Box{Pane: id}
	a.groupOf[id] = a.next
}

func (a *app) retitle(id, title string) {
	for i := range a.st.Panes {
		if a.st.Panes[i].ID == id && title != "" {
			a.st.Panes[i].Title = title
		}
	}
}

// windowTopic is what the program publishes the window's state to.
const windowTopic = "window"
