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
	github.com/creack/pty v1.1.24 // indirect
	github.com/danielgatis/go-utf8 v1.0.1 // indirect
	github.com/ebitengine/gomobile v0.0.0-20240518074828-e86332849895 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.8.3 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

// The GPU-side key-event pipeline (press/release/repeat + modifiers +
// InputSource correlation with text) only exists in unstablebuild's fork.
// The local copy adds one build-tag guard so it compiles for GOOS=windows;
// see /home/rdp/src/ebiten-ub/PATCH-NOTES.md.
replace github.com/hajimehoshi/ebiten/v2 => ../ebiten-ub
