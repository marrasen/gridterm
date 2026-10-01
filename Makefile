# Local build helpers, and what a release is made of.
#
# Every build is pure Go. No C toolchain, no development headers, and no
# platform can only be built on itself: cgo is off everywhere, so both
# releases cross-compile from either machine.

export CGO_ENABLED = 0

GO_WIN = GOOS=windows GOARCH=amd64 go

# VERSION is what the build calls itself. A tag when there is one, the
# commit when there is not, so a binary handed to somebody can always be
# got back to. CI passes the tag it is building.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
STAMP    = -X github.com/marrasen/kakel/internal/build.version=$(VERSION)

# -trimpath keeps the building machine's directory layout out of the
# binary, so two people building the same tag get the same bytes.
BUILD = go build -trimpath -ldflags "$(STAMP)"
DIST  = dist

.PHONY: all test vet fmt icon windows linux release clean
all: test windows

# The whole suite runs without a display: the window's tests draw into
# gunim's offscreen window.
test:
	go test ./...

vet:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
	# session has Unix files the Windows build never sees.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./session/...

fmt:
	gofmt -l .

# -H=windowsgui links the Windows build as a windowed program: started
# from a shortcut, Windows gives it no console window, which would flash
# up before the program let it go.
windows:
	$(GO_WIN) build -ldflags "-H=windowsgui" -o kakel.exe .

linux:
	GOOS=linux GOARCH=amd64 go build -o kakel-linux .

# The executable's own icon, for Explorer and a pinned shortcut. Only
# needed when the drawing changes, and a test fails when it has changed
# and this has not been run. See "The icon" in the README.
icon:
	go run github.com/akavel/rsrc@v0.10.2 -ico "$$(go run ./internal/mkico)" -arch amd64 -o rsrc_windows_amd64.syso

# Everything a release ships, into dist/. Run by CI on a tag, and by
# hand to see what a release would contain.
#
# Both cross-compile, so this makes a whole release wherever it is run.
release: clean
	mkdir -p $(DIST)
	$(GO_WIN) build -trimpath -ldflags "$(STAMP) -H=windowsgui" -o $(DIST)/kakel.exe .
	cd $(DIST) && zip -q kakel_$(VERSION)_windows_amd64.zip kakel.exe
	rm $(DIST)/kakel.exe
	GOOS=linux GOARCH=amd64 $(BUILD) -o $(DIST)/kakel .
	cd $(DIST) && tar czf kakel_$(VERSION)_linux_amd64.tar.gz kakel
	rm $(DIST)/kakel
	cd $(DIST) && sha256sum * > SHA256SUMS
	cat $(DIST)/SHA256SUMS

clean:
	rm -rf $(DIST)
