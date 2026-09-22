// Package update asks GitHub which release of gridterm is the newest,
// for the button on the about dialog.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gridterm/internal/build"
)

// repo is the project a release comes from, as GitHub names it.
const repo = "marrasen/gridterm"

// Releases is the page every release is listed on, which is where
// somebody is sent when the release GitHub named carries no page of its
// own.
const Releases = "https://github.com/" + repo + "/releases"

// api answers with the newest release.
//
// A variable so a test can answer the question itself. Every other way
// of testing this asks GitHub from whoever is running the tests.
var api = "https://api.github.com/repos/" + repo + "/releases/latest"

// patience is how long GitHub is given to answer. Short: somebody
// pressed a button and is watching the window while it waits.
const patience = 10 * time.Second

// readLimit is how much of an answer is read. The one this asks for is
// a few kilobytes, and the limit is what keeps a machine answering with
// an endless body from filling this window's memory.
const readLimit = 1 << 20

// Release is one release of gridterm, as GitHub describes it.
type Release struct {
	// Version is the tag the release was cut from: "v0.1.0".
	Version string

	// Page is where that release is downloaded from.
	Page string
}

// Latest is the newest release GitHub knows about.
//
// It is a network round trip to a machine that may be slow or
// unreachable, so it is called away from the goroutine that draws.
func Latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, patience)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Who is asking: GitHub refuses a request that names nobody.
	req.Header.Set("User-Agent", build.Name+"/"+build.Version())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("ask GitHub for the newest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}

	var said struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, readLimit)).Decode(&said); err != nil {
		return Release{}, fmt.Errorf("read what GitHub answered: %w", err)
	}
	tag := strings.TrimSpace(said.Tag)
	if tag == "" {
		return Release{}, errors.New("GitHub named no release")
	}
	page := strings.TrimSpace(said.Page)
	if page == "" {
		page = Releases
	}
	return Release{Version: tag, Page: page}, nil
}

// Standing is how a build compares to a release.
type Standing int

const (
	// Unknown is the two having no order between them, because one of
	// them is not a plain vX.Y.Z: a build from a working tree calls
	// itself dev-<commit>, and a pre-release carries a suffix. Such a
	// build may hold work that is in no release and lack work that
	// every release has, so there is no answer to give.
	Unknown Standing = iota

	// Behind is the release being the later version.
	Behind

	// Current is the build being that release.
	Current

	// Ahead is the build being the later version, which is what
	// somebody building from main has between releases.
	Ahead
)

// Against says how the build calling itself have compares to release.
func Against(have, release string) Standing {
	mine, ok := numbers(have)
	if !ok {
		return Unknown
	}
	theirs, ok := numbers(release)
	if !ok {
		return Unknown
	}
	for i := range mine {
		switch {
		case theirs[i] > mine[i]:
			return Behind
		case theirs[i] < mine[i]:
			return Ahead
		}
	}
	return Current
}

// numbers takes vX.Y.Z apart, and says so when a version is some other
// shape: a pre-release, a commit, or a word.
func numbers(version string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(parts) != len(out) {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		// Written back out to reject "01" and "+1", which Atoi reads and
		// no tag carries.
		if err != nil || n < 0 || strconv.Itoa(n) != part {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
