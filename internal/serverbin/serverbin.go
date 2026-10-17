// Package serverbin carries compiled copies of cmd/website-server, one per
// platform, so exported websites can include a server to host themselves.
//
// The binaries aren't in git: `go run ./tools/build` compiles them into bin/
// before building Sitesmith. A plain `go build` still works; the export
// option just reports that no servers are included.
package serverbin

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"io"
	"io/fs"
	"runtime"
)

//go:embed bin
var files embed.FS

type Target struct {
	ID        string `json:"id"` // GOOS-GOARCH
	Label     string `json:"label"`
	OS        string `json:"os"`
	Available bool   `json:"available"`
}

// Targets lists the platforms the build script compiles for, in the order
// the export dialog offers them.
var Targets = []Target{
	{ID: "windows-amd64", Label: "Windows", OS: "windows"},
	{ID: "darwin-arm64", Label: "Mac (Apple silicon: M1 and newer)", OS: "darwin"},
	{ID: "darwin-amd64", Label: "Mac (Intel)", OS: "darwin"},
	{ID: "linux-amd64", Label: "Linux (most servers and PCs)", OS: "linux"},
	{ID: "linux-arm64", Label: "Raspberry Pi and other ARM Linux (64-bit)", OS: "linux"},
}

// FileName is what the server is called inside an export.
func FileName(t Target) string {
	if t.OS == "windows" {
		return "website-server.exe"
	}
	return "website-server"
}

func embedded(id string) string { return "bin/website-server-" + id + ".gz" }

// List returns every target, marking which ones this build of Sitesmith carries.
func List() []Target {
	out := make([]Target, len(Targets))
	for i, t := range Targets {
		_, err := fs.Stat(files, embedded(t.ID))
		t.Available = err == nil
		out[i] = t
	}
	return out
}

// Default is the target matching the computer Sitesmith runs on.
func Default() string { return runtime.GOOS + "-" + runtime.GOARCH }

var ErrUnavailable = errors.New("this copy of Sitesmith was built without a web server for that platform")

// Binary returns the decompressed server for a target.
func Binary(id string) (Target, []byte, error) {
	for _, t := range Targets {
		if t.ID != id {
			continue
		}
		gz, err := files.ReadFile(embedded(id))
		if err != nil {
			return t, nil, ErrUnavailable
		}
		zr, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			return t, nil, err
		}
		data, err := io.ReadAll(zr)
		return t, data, err
	}
	return Target{}, nil, ErrUnavailable
}
