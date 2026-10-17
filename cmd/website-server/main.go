// website-server hosts a website exported from Sitesmith. Put it next to the
// exported "site" folder and run it; there's nothing to install or configure.
//
// It is deliberately tiny and has no dependencies beyond the standard library,
// because a copy of it ships inside every exported website.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	dir := flag.String("dir", "", `folder that holds the website (default: the "site" folder next to this program)`)
	port := flag.Int("port", 0, "port to listen on (default: $PORT, or 8080)")
	local := flag.Bool("local", false, "only accept visitors from this computer")
	openBrowser := flag.Bool("open", false, "open the website in your browser on start")
	flag.Parse()

	if err := run(*dir, *port, *local, *openBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "\n  Couldn't start the website:", err)
		// Double-clicked on Windows, the window would vanish before anyone could read this.
		if runtime.GOOS == "windows" {
			fmt.Fprintln(os.Stderr, "\n  Press Enter to close.")
			fmt.Scanln()
		}
		os.Exit(1)
	}
}

func run(dir string, port int, local, openBrowser bool) error {
	root, err := siteDir(dir)
	if err != nil {
		return err
	}

	explicit := port != 0
	if !explicit {
		if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
			port, explicit = p, true
		} else {
			port = 8080
		}
	}
	host := ""
	if local {
		host = "127.0.0.1"
	}
	ln, err := listen(host, port, explicit)
	if err != nil {
		return err
	}
	port = ln.Addr().(*net.TCPAddr).Port

	url := fmt.Sprintf("http://localhost:%d/", port)
	fmt.Printf("\n  Your website is running.\n\n  On this computer:    %s\n", url)
	if !local {
		if ip := lanIP(); ip != "" {
			fmt.Printf("  On your network:     http://%s:%d/\n", ip, port)
		}
	}
	fmt.Printf("  Serving files from:  %s\n\n  Close this window or press Ctrl+C to stop.\n\n", root)

	if openBrowser && (runtime.GOOS == "windows" || runtime.GOOS == "darwin") {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openURL(url)
		}()
	}

	srv := &http.Server{
		Handler: Handler(root),
		// Limits that keep a slow or malicious client from tying up the server.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	return srv.Serve(ln)
}

// siteDir finds the website: the -dir flag, else "site" next to this program,
// else "site" in the current folder.
func siteDir(dir string) (string, error) {
	var candidates []string
	if dir != "" {
		candidates = []string{dir}
	} else {
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			candidates = append(candidates, filepath.Join(filepath.Dir(exe), "site"))
		}
		candidates = append(candidates, "site")
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf(`no website found. Put this program next to the "site" folder from your download (looked in %s)`,
		strings.Join(candidates, ", "))
}

// listen uses the requested port. If the port was only a default and it's
// taken, the next free one is used instead.
func listen(host string, port int, explicit bool) (net.Listener, error) {
	tries := 20
	if explicit {
		tries = 1
	}
	var err error
	for i := 0; i < tries; i++ {
		var ln net.Listener
		ln, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port+i)))
		if err == nil {
			return ln, nil
		}
		if !isAddrInUse(err) {
			return nil, err
		}
	}
	if explicit {
		return nil, fmt.Errorf("port %d is already in use; pick another with -port", port)
	}
	return nil, fmt.Errorf("no free port between %d and %d", port, port+tries-1)
}

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var se *os.SyscallError
	// WSAEADDRINUSE on Windows.
	return errors.As(err, &se) && se.Err == syscall.Errno(10048)
}

// lanIP is this computer's address on the local network, for visitors on
// the same Wi-Fi.
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.IsPrivate() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

func openURL(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	}
}

// Handler serves a static website folder:
//   - /about/ serves about/index.html, and /about redirects to /about/
//   - missing pages get 404.html with a 404 status
//   - no folder listings, no hidden files, nothing outside the folder
func Handler(root string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		clean := path.Clean("/" + r.URL.Path)
		rel := strings.TrimPrefix(clean, "/")
		if rel == "" {
			rel = "."
		}
		// IsLocal also rejects Windows device names like CON and NUL.
		if !filepath.IsLocal(filepath.FromSlash(rel)) || hasHiddenPart(rel) {
			notFound(w, r, root)
			return
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		fi, err := os.Stat(full)
		if err != nil {
			notFound(w, r, root)
			return
		}
		if fi.IsDir() {
			if !strings.HasSuffix(r.URL.Path, "/") {
				target := r.URL.Path + "/"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			full = filepath.Join(full, "index.html")
			if fi, err = os.Stat(full); err != nil || fi.IsDir() {
				notFound(w, r, root)
				return
			}
		}

		switch {
		case strings.HasPrefix(clean, "/media/"):
			// Image file names are unique per upload, so they never change.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		case strings.HasPrefix(clean, "/assets/"):
			// The stylesheet link carries a version number that changes with the design.
			h.Set("Cache-Control", "public, max-age=86400")
		default:
			// Pages: always check for a newer copy, so updates show up straight away.
			h.Set("Cache-Control", "no-cache")
		}
		f, err := os.Open(full)
		if err != nil {
			notFound(w, r, root)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	})
}

func hasHiddenPart(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") && part != "." {
			return true
		}
	}
	return false
}

func notFound(w http.ResponseWriter, r *http.Request, root string) {
	page, err := os.ReadFile(filepath.Join(root, "404.html"))
	w.Header().Set("Cache-Control", "no-cache")
	if err != nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
}
