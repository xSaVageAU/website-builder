// Command build makes a release build of Sitesmith:
//
//  1. compiles cmd/website-server for every platform in serverbin.Targets,
//  2. gzips each into internal/serverbin/bin/ (git-ignored),
//  3. builds Sitesmith, which embeds those servers.
//
// Run it from the repository root:
//
//	go run ./tools/build                         # Sitesmith for this computer
//	go run ./tools/build -goos linux -goarch arm64
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sitesmith/internal/serverbin"
)

func main() {
	goos := flag.String("goos", runtime.GOOS, "operating system to build Sitesmith for")
	goarch := flag.String("goarch", runtime.GOARCH, "CPU architecture to build Sitesmith for")
	out := flag.String("o", "", "output file (default dist/sitesmith-<os>-<arch>[.exe])")
	flag.Parse()
	log.SetFlags(0)

	if _, err := os.Stat("go.mod"); err != nil {
		log.Fatal("run this from the repository root: go run ./tools/build")
	}
	start := time.Now()

	binDir := filepath.Join("internal", "serverbin", "bin")
	tmp, err := os.MkdirTemp("", "sitesmith-build-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	for _, t := range serverbin.Targets {
		goosT, goarchT, _ := strings.Cut(t.ID, "-")
		raw := filepath.Join(tmp, t.ID)
		if err := goBuild(goosT, goarchT, raw, "./cmd/website-server"); err != nil {
			log.Fatalf("website-server for %s: %v", t.ID, err)
		}
		gz := filepath.Join(binDir, "website-server-"+t.ID+".gz")
		size, err := gzipFile(raw, gz)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("  website-server  %-14s %5.1f MB (compressed)", t.ID, float64(size)/(1<<20))
	}

	if *out == "" {
		*out = filepath.Join("dist", fmt.Sprintf("sitesmith-%s-%s", *goos, *goarch))
		if *goos == "windows" {
			*out += ".exe"
		}
	}
	if err := goBuild(*goos, *goarch, *out, "."); err != nil {
		log.Fatalf("sitesmith: %v", err)
	}
	fi, _ := os.Stat(*out)
	log.Printf("  sitesmith       %-14s %5.1f MB  ->  %s", *goos+"-"+*goarch, float64(fi.Size())/(1<<20), *out)
	log.Printf("done in %s", time.Since(start).Round(time.Millisecond))
}

// goBuild makes a small, static, reproducible binary.
func goBuild(goos, goarch, out, pkg string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, pkg)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func gzipFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if _, err := io.Copy(zw, in); err != nil {
		f.Close()
		return 0, err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	fi, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
