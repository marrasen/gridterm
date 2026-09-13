module github.com/marcus/gridterm

go 1.25.0

require (
	github.com/hajimehoshi/ebiten/v2 v2.7.5
	golang.org/x/image v0.45.0
)

require (
	github.com/ebitengine/gomobile v0.0.0-20240518074828-e86332849895 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.8.3 // indirect
	github.com/jezek/xgb v1.1.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// The GPU-side key-event pipeline (press/release/repeat + modifiers +
// InputSource correlation with text) only exists in unstablebuild's fork.
// The local copy adds one build-tag guard so it compiles for GOOS=windows;
// see /home/rdp/src/ebiten-ub/PATCH-NOTES.md.
replace github.com/hajimehoshi/ebiten/v2 => ../ebiten-ub
