module github.com/marrasen/gridterm

go 1.27.1

require (
	github.com/atotto/clipboard v0.1.4
	github.com/aymanbagabas/go-pty v0.2.3
	github.com/danielgatis/go-vte v1.0.11
	github.com/hajimehoshi/ebiten/v2 v2.10.2
	github.com/marrasen/gunim v0.0.0-20260925163618-53c34a99f93c
	github.com/pkg/sftp v1.13.11
	github.com/rivo/uniseg v0.4.7
	golang.design/x/clipboard v0.9.0
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.45.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/creack/pty v1.1.24 // indirect
	github.com/danielgatis/go-utf8 v1.0.1 // indirect
	github.com/ebitengine/gomobile v0.0.0-20260820040257-d11f821a26a6 // indirect
	github.com/ebitengine/hideconsole v1.0.0 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	golang.design/x/x11 v0.2.0 // indirect
	golang.org/x/exp/shiny v0.0.0-20250606033433-dcc06ee1d476 // indirect
	golang.org/x/mobile v0.0.0-20250606033058-a2a15c67f36f // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

// The key-event pipeline — press, release and repeat with modifiers,
// correlated with the text a keystroke produced — is not in upstream
// ebitengine. Its polled IsKeyPressed cannot tell Ctrl+C from the letter
// c, which rules out writing a terminal against it.
//
// marrasen/ebiten is upstream v2.10.2 with that pipeline added, and the
// real paths of dropped files reported beside the filesystem they arrive
// as. It is 404 lines across six files, all additive: Runes is still
// filled, the polled key state is unchanged, and nothing reading the old
// API sees a difference. See AppendInputEvents in that fork's input.go.
//
// This replaced a fork of unstablebuild's fork of v2.7.5, which carried
// the same feature in patched GLFW C sources. Upstream rewrote that
// layer in Go for v2.10, so none of it needs C any more: gridterm builds
// with CGO_ENABLED=0 on every platform now.
replace github.com/hajimehoshi/ebiten/v2 => github.com/marrasen/ebiten/v2 v2.10.2-gt.1
