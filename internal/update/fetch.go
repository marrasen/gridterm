package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/marrasen/kakel/internal/build"
)

// Fetching a release: the archive for this system, checked against the
// release's SHA256SUMS, and the program in it written beside the one
// running, as exe.new, for install.Replace to put in its place.

// archiveLimit is the largest archive taken, and fetchPatience how long
// a fetch waits.
const (
	archiveLimit  = 200 << 20
	fetchPatience = 5 * time.Minute
)

// ArchiveName is the name of the release's archive for goos and goarch,
// as `make release` names it.
func ArchiveName(version, goos, goarch string) string {
	if goos == "windows" {
		return "kakel_" + version + "_" + goos + "_" + goarch + ".zip"
	}
	return "kakel_" + version + "_" + goos + "_" + goarch + ".tar.gz"
}

// Fetch downloads r's archive for goos and goarch, checks it against the
// release's SHA256SUMS, and writes the program in it to exe + ".new",
// which it returns.
func Fetch(ctx context.Context, r Release, goos, goarch, exe string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchPatience)
	defer cancel()
	name := ArchiveName(r.Version, goos, goarch)
	at, ok := r.Assets[name]
	if !ok {
		return "", fmt.Errorf("%s has no %s", r.Version, name)
	}
	sumsAt, ok := r.Assets["SHA256SUMS"]
	if !ok {
		return "", fmt.Errorf("%s has no SHA256SUMS to check it against", r.Version)
	}
	sums, err := get(ctx, sumsAt, readLimit)
	if err != nil {
		return "", fmt.Errorf("fetch SHA256SUMS: %w", err)
	}
	want, err := sumOf(sums, name)
	if err != nil {
		return "", err
	}
	archive, err := get(ctx, at, archiveLimit)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", name, err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s is not what SHA256SUMS says it is", name)
	}
	program := "kakel"
	if goos == "windows" {
		program = "kakel.exe"
	}
	body, err := unpack(archive, name, program)
	if err != nil {
		return "", err
	}
	part := exe + ".new"
	if err := os.WriteFile(part, body, 0o755); err != nil {
		return "", err
	}
	return part, nil
}

// get fetches url, at most limit bytes of it.
func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", build.Name+"/"+build.Version())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the server answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("it is bigger than any kakel")
	}
	return body, nil
}

// sumOf is the checksum SHA256SUMS gives name.
func sumOf(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS says nothing of %s", name)
}

// unpack is the file called program in archive.
func unpack(archive []byte, name, program string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if f.Name == program {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer func() { _ = rc.Close() }()
				return io.ReadAll(io.LimitReader(rc, archiveLimit))
			}
		}
		return nil, fmt.Errorf("%s holds no %s", name, program)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("%s holds no %s", name, program)
		}
		if h.Name == program && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, archiveLimit))
		}
	}
}
