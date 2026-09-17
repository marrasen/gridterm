# Local build helpers.
#
# Windows is the primary target and needs no C toolchain at all.
# Building for Linux needs the X11 headers ebitengine's bundled GLFW
# compiles against; XDEV points at a local unpack of them for machines
# where they are not installed system-wide (see README).
XDEV ?= /tmp/xdev

GO_LINUX = CGO_ENABLED=1 \
	CGO_CFLAGS="-I$(XDEV)/root/usr/include" \
	CGO_LDFLAGS="-L$(XDEV)/lib" \
	go

GO_WIN = GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go

.PHONY: test windows linux vet fmt icon all
all: test windows

# Everything testable without a display, which is most of the logic.
# glyph is here because its tests only cover font selection. render and
# main are here because ebiten allocates textures without a window: they
# drive the widget tree and the compositor and check the code path and
# the frame accounting, not the pixels, which still need a real window
# to judge.
test:
	go test . ./agent ./appicon ./conf ./conns ./glyph ./grid/... ./input ./internal/... ./jobs ./keys ./mcp ./meter ./remote ./render ./serve ./settings ./shells ./themes ./ui/... ./vfs/... ./vt/... ./session/...

vet:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
	# session has Unix files the Windows build never sees.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./session/...

fmt:
	gofmt -l .

windows:
	$(GO_WIN) build -o gridterm.exe .

# The executable's own icon, for Explorer and a pinned shortcut. Only
# needed when the drawing changes, and a test fails when it has changed
# and this has not been run. See "The icon" in the README.
icon:
	go run github.com/akavel/rsrc@v0.10.2 -ico "$$(go run ./internal/mkico)" -arch amd64 -o rsrc_windows_amd64.syso

linux:
	$(GO_LINUX) build -o gridterm-linux .
