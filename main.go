// Sitesmith is a website builder that runs as a single program on your own
// computer and publishes plain, fast static files you can host anywhere.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"sitesmith/internal/render"
	"sitesmith/internal/server"
	"sitesmith/web"
)

func main() {
	dataDir := flag.String("data", "site-data", "folder that holds your sites (one subfolder per site)")
	addr := flag.String("addr", "127.0.0.1:4321", "address for the editor (it only accepts local connections)")
	noOpen := flag.Bool("no-browser", false, "don't open the editor in a browser on start")
	flag.Parse()

	r, err := render.New()
	if err != nil {
		log.Fatal(err)
	}
	ws, err := server.OpenWorkspace(*dataDir, r)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	admin, err := fs.Sub(web.Admin, "admin")
	if err != nil {
		log.Fatal(err)
	}
	srv := server.New(ws, r, admin)

	ln, err := listen(*addr)
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	abs, _ := filepath.Abs(*dataDir)
	fmt.Printf("\n  Sitesmith is running.\n\n  Editor:     %s\n  Your files: %s\n\n  Close this window to stop.\n\n", url, abs)

	if !*noOpen {
		go func() {
			time.Sleep(300 * time.Millisecond)
			_ = server.OpenPath(url)
		}()
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(hs.Serve(ln))
}

// listen tries the requested port and, if it's taken (say, a second copy is
// running), the next few after it.
func listen(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var p int
	fmt.Sscan(port, &p)
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(p+i)))
		if err == nil {
			return ln, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) && !isAddrInUse(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no free port near %s", addr)
}

func isAddrInUse(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		var se *os.SyscallError
		if errors.As(op.Err, &se) {
			// WSAEADDRINUSE on Windows.
			return se.Err == syscall.Errno(10048)
		}
	}
	return false
}
