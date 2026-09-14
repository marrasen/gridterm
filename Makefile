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

# Everything testable without a display, which is most of the logic. The
# glyph and main tests are here too: those packages need a GPU to draw,
# but their tests only cover font selection, which does not.
test:
	go test . ./glyph ./grid/... ./input ./vt/... ./session/...

vet:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -l .

windows:
	$(GO_WIN) build -o gridterm.exe .

linux:
	$(GO_LINUX) build -o gridterm-linux .
