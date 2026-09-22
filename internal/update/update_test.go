package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serving points the package at a server of the test's own, so nothing
// here asks GitHub.
func serving(t *testing.T, handle http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handle)
	t.Cleanup(srv.Close)
	was := api
	t.Cleanup(func() { api = was })
	api = srv.URL
}

// The newest release is read out of what GitHub answers.
func TestTheNewestReleaseIsRead(t *testing.T) {
	var asked *http.Request
	serving(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r
		_, _ = w.Write([]byte(`{
			"tag_name": "v0.2.0",
			"html_url": "https://github.com/marrasen/gridterm/releases/tag/v0.2.0",
			"name": "v0.2.0"
		}`))
	})

	got, err := Latest(context.Background())
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if got.Version != "v0.2.0" {
		t.Errorf("it is %q, want the tag", got.Version)
	}
	if got.Page != "https://github.com/marrasen/gridterm/releases/tag/v0.2.0" {
		t.Errorf("its page is %q", got.Page)
	}
	// GitHub refuses a request that names nobody, so every build has to
	// say who it is.
	if asked.Header.Get("User-Agent") == "" {
		t.Error("the request named nobody")
	}
}

// A release GitHub named no page for is still offered, against the page
// every release is listed on.
func TestAReleaseWithNoPageOfItsOwnFallsBackToTheList(t *testing.T) {
	serving(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": "v0.2.0"}`))
	})

	got, err := Latest(context.Background())
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if got.Page != Releases {
		t.Errorf("its page is %q, want %q", got.Page, Releases)
	}
}

// An answer that is not one is a failure, rather than a release with no
// name that the dialog would then compare against.
func TestAnAnswerThatNamesNoReleaseFails(t *testing.T) {
	for _, one := range []struct {
		name string
		code int
		body string
	}{
		{name: "nothing there", code: http.StatusNotFound, body: `{"message":"Not Found"}`},
		{name: "rate limited", code: http.StatusForbidden, body: `{"message":"API rate limit exceeded"}`},
		{name: "not json", code: http.StatusOK, body: `<!doctype html>`},
		{name: "no tag", code: http.StatusOK, body: `{"html_url":"https://example.invalid"}`},
	} {
		t.Run(one.name, func(t *testing.T) {
			serving(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(one.code)
				_, _ = w.Write([]byte(one.body))
			})

			if got, err := Latest(context.Background()); err == nil {
				t.Errorf("it handed back %+v, want a failure", got)
			}
		})
	}
}

// A context that is already cancelled is not asked about: the window is
// closing and nothing is waiting for the answer.
func TestAskingStopsWithTheWindow(t *testing.T) {
	serving(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Latest(ctx); err == nil {
		t.Error("it asked anyway")
	}
}

// How a build compares to the newest release.
func TestHowABuildComparesToARelease(t *testing.T) {
	for _, one := range []struct {
		have, release string
		want          Standing
	}{
		{have: "v0.1.0", release: "v0.2.0", want: Behind},
		{have: "v0.1.0", release: "v0.1.1", want: Behind},
		{have: "v0.9.9", release: "v1.0.0", want: Behind},
		{have: "v0.1.0", release: "v0.1.0", want: Current},
		// Built from main after a release was cut.
		{have: "v0.3.0", release: "v0.2.0", want: Ahead},
		// Ten comes after nine, which it would not if these were read
		// as text.
		{have: "v0.9.0", release: "v0.10.0", want: Behind},
		// Without the v, which is how a version is sometimes written.
		{have: "0.1.0", release: "0.2.0", want: Behind},
		// Neither of these has a place in the order.
		{have: "dev-3e62f4547e66", release: "v0.2.0", want: Unknown},
		{have: "v0.1.0", release: "v0.2.0-rc1", want: Unknown},
		{have: "v0.1.0", release: "v0.2", want: Unknown},
		{have: "v0.1.0", release: "v0.02.0", want: Unknown},
		{have: "v0.1.0", release: "", want: Unknown},
	} {
		if got := Against(one.have, one.release); got != one.want {
			t.Errorf("Against(%q, %q) = %v, want %v",
				one.have, one.release, got, one.want)
		}
	}
}
