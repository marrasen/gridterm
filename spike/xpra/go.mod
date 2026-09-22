// A spike, not part of gridterm. Its own module so that go-xpra stays
// out of the top-level go.mod and out of every release build.
//
// The replace below points at a fork carrying ui.DesktopResized and the
// display-configure packet it sends. Released go-xpra tells the server
// its desktop size once, in the hello, and never again -- which is fine
// for a desktop whose monitors do not move and wrong for a backend whose
// desktop is one window somebody is dragging. See "The desktop size" in
// README.md.
module github.com/marrasen/gridterm/spike/xpra

go 1.27.1

require (
	github.com/Xpra-org/go-xpra v0.2.2
	github.com/marrasen/gridterm v0.0.0
)

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	golang.org/x/image v0.45.0 // indirect
)

replace github.com/Xpra-org/go-xpra => github.com/marrasen/go-xpra v0.2.3-0.20260922062853-a877331b6fb4

replace github.com/marrasen/gridterm => ../..
