module github.com/marrasen/kakel

go 1.27.1

require (
	github.com/atotto/clipboard v0.1.4
	github.com/aymanbagabas/go-pty v0.2.3
	// marrasen/go-vte is upstream danielgatis/go-vte v1.0.11 with two
	// changes. The parser hands a performer its own params and
	// intermediates, reused from one sequence to the next, where it
	// allocated them for every sequence: a screen of true colour half
	// blocks sends two sequences a cell. vt reads them only during the
	// call. And SOS, PM and APC strings stop growing at a megabyte, which
	// upstream's own test expected.
	github.com/marrasen/go-vte v1.0.11-gt.2
	github.com/marrasen/gunim v0.0.0-20261002161936-0b5b33d0c29a
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
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	golang.design/x/x11 v0.2.0 // indirect
	golang.org/x/exp/shiny v0.0.0-20250606033433-dcc06ee1d476 // indirect
	golang.org/x/mobile v0.0.0-20250606033058-a2a15c67f36f // indirect
	golang.org/x/text v0.42.0 // indirect
)
