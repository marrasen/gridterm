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

.PHONY: test windows linux vet fmt all
all: test windows

# Everything testable without a display, which is most of the logic.
# glyph is here because its tests only cover font selection. render and
# main are here because ebiten allocates textures without a window: they
# drive the widget tree and the compositor and check the code path and
# the frame accounting, not the pixels, which still need a real window
# to judge.
test:
	go test . ./conns ./glyph ./grid/... ./input ./internal/... ./meter ./agent ./mcp ./remote ./serve ./jobs ./render ./settings ./shells ./ui/... ./vfs/... ./vt/... ./session/...

vet:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
	# session has Unix files the Windows build never sees.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./session/...

fmt:
	gofmt -l .

windows:
	$(GO_WIN) build -o gridterm.exe .

linux:
	$(GO_LINUX) build -o gridterm-linux .
