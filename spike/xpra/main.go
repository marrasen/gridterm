// Command xpraspike connects to an xpra server and reports what the
// protocol hands a client: the windows it makes, the frames it paints
// into them, and what a click or a keystroke sends back.
//
// It is step 2 of REMOTE-APPS.md -- standing go-xpra up outside
// gridterm -- and it draws nothing. Every window is a buffer in memory
// that is written out as a PNG whenever its pixels change, so the whole
// thing runs over SSH on a machine with no display.
//
// Against a real server:
//
//	xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
//	go run . tcp://127.0.0.1:14500/
//
// Against the fake one that ships inside go-xpra, which needs no xpra
// installed anywhere:
//
//	go run github.com/Xpra-org/go-xpra/internal/mockserver@v0.2.2 &
//	go run . tcp://127.0.0.1:14500/
//
// This is a nested module on purpose. gridterm's own go.mod does not
// name go-xpra, so nothing here is in the release build, and `go build
// ./...` at the top of the repo does not see it.
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Xpra-org/go-xpra/client"
	"github.com/Xpra-org/go-xpra/protocol"

	"github.com/marrasen/gridterm/input"
)

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	serve := flag.String("serve", "", "run the fake server on this address instead of connecting")
	out := flag.String("out", "frames", "directory the PNGs are written to")
	chordSpec := flag.String("chord", "", "press these after typing, as in \"ctrl+a,ctrl+c\"")
	copyText := flag.String("copy", "", "announce this as the local clipboard once connected")
	closeWindows := flag.Bool("close", false, "ask the server to close its windows before hanging up, ending the session")
	layoutName := flag.String("layout", "us", "ask the server to load this XKB layout; its keysyms are the only ones that can be typed")
	typeText := flag.String("type", "Hello, World! 42", "type this into the first window once it is focused")
	clickAt := flag.String("click", "", "click here instead of the middle of the first window, as x,y in desktop pixels")
	drive := flag.Bool("drive", true, "send a scripted click, keystroke and resize once the first frame arrives")
	quit := flag.Duration("quit", 15*time.Second, "give up and disconnect after this long; 0 waits for ever")
	verbose := flag.Bool("v", false, "log every packet go-xpra does not handle")
	flag.Parse()

	if *serve != "" {
		if err := serveOn(*serve); err != nil {
			log.Fatal(err)
		}
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [flags] tcp://host:port/\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s -serve 127.0.0.1:14500\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}
	point, err := parsePoint(*clickAt)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	chords, err := parseChords(*chordSpec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *out, point, *typeText, *layoutName, *copyText, chords, *drive, *closeWindows, *quit, *verbose); err != nil {
		log.Fatal(err)
	}
}

// closeGrace is how long the server gets to act on a close request
// before the spike stops waiting.
const closeGrace = 3 * time.Second

func run(target, out string, click *image.Point, typeText, layoutName, copyText string, chords []input.Event, drive, closeWindows bool, quit time.Duration, verbose bool) error {
	address, err := tcpAddress(target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", out, err)
	}

	log.Printf("dialling %s", address)
	conn, err := protocol.Dial(address)
	if err != nil {
		return fmt.Errorf("dialling %s: %w", address, err)
	}

	display := newDisplay(out, drive)
	display.click = click
	display.text = typeText
	display.layoutName = layoutName
	display.copy = copyText
	display.chords = chords
	defer display.Close()

	// hungUp records that the spike ended the session itself, so that
	// the error the client reports for it is not called a failure.
	var hungUp atomic.Bool

	if quit > 0 {
		time.AfterFunc(quit, func() {
			if !closeWindows {
				// Just go away. The session outlives us, which is the
				// whole point of xpra and the thing "screen for X"
				// means -- so detaching, not closing, is what a client
				// normally does.
				log.Printf("the %s clock ran out, detaching", quit)
				hungUp.Store(true)
				display.Close()
				return
			}

			log.Printf("the %s clock ran out, closing every window", quit)
			display.closeEverything()

			// Closing is the server's decision: we ask, and the windows
			// go when it says so. A server that never answers -- one too
			// old to know the packet we asked with, which is what an
			// Xpra 3 server does with every packet this client sends --
			// would otherwise leave us waiting for ever.
			time.AfterFunc(closeGrace, func() {
				log.Printf("the server did not close its windows within %s, hanging up", closeGrace)
				hungUp.Store(true)
				display.Close()
			})
		})
	}

	err = client.New(conn, display, verbose, "", "").Run()
	display.report()
	if err != nil && hungUp.Load() {
		// We closed the desktop out from under the client on purpose.
		return nil
	}
	return err
}

// tcpAddress pulls host:port out of an xpra tcp:// URL, defaulting to
// xpra's own port.
//
// Only tcp:// for now. The other transports are a stream each and the
// client takes any of them, so adding one is a dial and no more -- and
// the one gridterm would really use is its own SSH session running
// `xpra _proxy`, which is protocol.New over a pipe pair rather than a
// URL at all.
func tcpAddress(target string) (string, error) {
	rest, ok := strings.CutPrefix(target, "tcp://")
	if !ok {
		return "", fmt.Errorf("%q: only tcp:// targets, for now", target)
	}
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return "", fmt.Errorf("%q names no host", target)
	}
	if _, _, err := net.SplitHostPort(rest); err != nil {
		rest = net.JoinHostPort(rest, "14500")
	}
	return rest, nil
}

// serveOn runs the fake server until it is killed, one client at a time.
//
// Serially on purpose: this exists to be watched, and two sessions
// interleaving their logs would only make that harder.
func serveOn(address string) error {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", address, err)
	}
	defer ln.Close()
	log.Printf("fake server listening on %s", ln.Addr())

	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		log.Printf("fake: client connected from %s", conn.RemoteAddr())
		(&fakeServer{}).serve(protocol.New(conn))
		log.Printf("fake: client gone, waiting for the next one")
	}
}

// parsePoint reads an "x,y" flag, and nil for an empty one.
//
// Aiming a click matters against a real application: the middle of a
// window is rarely a menu, and a menu is the case this whole spike is
// pointed at. xfontsel's field names sit near its top left.
func parsePoint(s string) (*image.Point, error) {
	if s == "" {
		return nil, nil
	}
	x, y, ok := strings.Cut(s, ",")
	if !ok {
		return nil, fmt.Errorf("-click %q: want x,y", s)
	}
	px, err := strconv.Atoi(strings.TrimSpace(x))
	if err != nil {
		return nil, fmt.Errorf("-click %q: %w", s, err)
	}
	py, err := strconv.Atoi(strings.TrimSpace(y))
	if err != nil {
		return nil, fmt.Errorf("-click %q: %w", s, err)
	}
	return &image.Point{X: px, Y: py}, nil
}
