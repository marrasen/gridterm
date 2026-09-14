module github.com/marcus/gridterm

go 1.26.0

require (
	github.com/aymanbagabas/go-pty v0.2.3
	github.com/danielgatis/go-vte v1.0.11
	github.com/hajimehoshi/ebiten/v2 v2.7.5
	github.com/rivo/uniseg v0.4.7
	golang.org/x/image v0.45.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/danielgatis/go-utf8 v1.0.1 // indirect
	github.com/ebitengine/gomobile v0.0.0-20240518074828-e86332849895 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.8.3 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

// The GPU-side key-event pipeline — press/release/repeat with modifiers,
// correlated with the text a keystroke produced — exists only in
// unstablebuild's fork of ebitengine. Upstream's polled IsKeyPressed
// cannot tell Ctrl+C from the letter c, which rules out writing a
// terminal against it.
//
// That fork does not compile for GOOS=windows: it calls glfw.InitHint,
// which the pure-Go Windows glfw port does not define. marrasen/ebiten
// is the same commit with that one call put behind a build tag. The fix
// is offered upstream as unstablebuild/ebiten#windows-build; once it
// lands, this line can point at the fork's own tag again.
replace github.com/hajimehoshi/ebiten/v2 => github.com/marrasen/ebiten/v2 v2.7.5-ub.27.win.1
