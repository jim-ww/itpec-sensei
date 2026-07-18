package mcpserver

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.ngrok.com/ngrok/v2"

	"github.com/jim-ww/itpec-sensei/internal/core"
)

// Options configures Run. ImageViewer defaults to "xdg-open" and Addr to
// "127.0.0.1:8790" if left zero-valued.
type Options struct {
	Remote      bool   // expose over Streamable HTTP instead of stdio
	Addr        string // local listen address for Remote
	UseNgrok    bool   // also forward a public ngrok tunnel to the Remote server
	ImageViewer string // local command open_question_image uses to open images on the machine running this server
}

// Run implements `itpec-sensei serve [--remote]`.
func Run(ctx context.Context, c *core.Core, opts Options) error {
	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:8790"
	}
	imageViewer := opts.ImageViewer
	if imageViewer == "" {
		imageViewer = "xdg-open"
	}

	sess := &sessionState{}
	server := mcp.NewServer(&mcp.Implementation{Name: "itpec-sensei", Version: "0.1.0"}, nil)

	imgFS, err := c.Bank.ImagesFS()
	if err != nil {
		return fmt.Errorf("images fs: %w", err)
	}

	if !opts.Remote {
		// Question images are always served as a URL too (get_next_question
		// and get_question also embed them as base64 tool content, but some
		// MCP clients, e.g. Claude web, don't render embedded image blocks to
		// the user even though the model can read them — a plain URL in a
		// markdown image link does). Over stdio there's no existing HTTP
		// server to point at, so spin up a local-only one (OS-assigned port)
		// just for images.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("listen for image server: %w", err)
		}
		defer ln.Close()
		imgMux := http.NewServeMux()
		imgMux.Handle("/images/", http.StripPrefix("/images/", imageHandler(imgFS)))
		go func() {
			if err := http.Serve(ln, imgMux); err != nil && err != http.ErrServerClosed {
				log.Printf("local image server stopped: %v", err)
			}
		}()

		var baseURL atomic.Pointer[string]
		local := "http://" + ln.Addr().String()
		baseURL.Store(&local)
		registerTools(server, c, sess, &baseURL, imageViewer)

		err = server.Run(ctx, &mcp.StdioTransport{})
		endMCPSession(c, sess)
		return err
	}

	var baseURL atomic.Pointer[string]
	local := "http://" + addr
	baseURL.Store(&local)
	registerTools(server, c, sess, &baseURL, imageViewer)

	// The SDK's default DNS-rebinding protection rejects any request whose
	// Host header isn't localhost, since the server binds to a loopback
	// address. With --ngrok that's exactly what legitimate forwarded
	// requests look like (Host: <subdomain>.ngrok-free.app), so it has to be
	// disabled in that case — the tunnel itself is the intentional exposure.
	mux := http.NewServeMux()
	mux.Handle("/", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{DisableLocalhostProtection: opts.UseNgrok}))
	mux.Handle("/images/", http.StripPrefix("/images/", imageHandler(imgFS)))

	httpServer := &http.Server{Addr: addr, Handler: mux}
	go func() {
		log.Printf("MCP server listening on http://%s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local HTTP server stopped: %v", err)
		}
	}()

	if !opts.UseNgrok {
		<-ctx.Done()
		endMCPSession(c, sess)
		return httpServer.Close()
	}

	// Forward a public ngrok endpoint to the local server, using the ngrok-go
	// SDK (NGROK_AUTHTOKEN env var) rather than shelling out to the ngrok binary.
	// NGROK_RESERVED_URL pins this to our reserved domain instead of a random one.
	var endpointOpts []ngrok.EndpointOption
	if reservedURL := os.Getenv("NGROK_RESERVED_URL"); reservedURL != "" {
		endpointOpts = append(endpointOpts, ngrok.WithURL(reservedURL))
	}
	fwd, err := ngrok.Forward(ctx, ngrok.WithUpstream("http://"+addr), endpointOpts...)
	if err != nil {
		log.Printf("ngrok tunnel not started: %v (serving locally on %s only)", err, addr)
		<-ctx.Done()
		endMCPSession(c, sess)
		return httpServer.Close()
	}
	defer fwd.Close()
	publicURL := fwd.URL().String()
	baseURL.Store(&publicURL)
	log.Printf("MCP server publicly reachable at %s", publicURL)

	<-ctx.Done()
	fwd.Close()
	endMCPSession(c, sess)
	return httpServer.Close()
}

// imageHandler serves question images from imgFS (the bank's embedded
// "images" subtree) with colors inverted by default (dark mode), matching
// the inline embedding's default. Pass ?light=1 to get the original colors.
func imageHandler(imgFS fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		f, err := imgFS.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			http.Error(w, fmt.Sprintf("decode image: %v", err), http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("light") != "1" {
			img = core.InvertImage(img)
		}
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, img); err != nil {
			log.Printf("encode image %s: %v", name, err)
		}
	})
}

// sessionState tracks the single lazily-started progress-DB session shared by
// all tool calls in this server process (see get_next_question in
// tools_question.go), plus the path of the last image opened via
// open_question_image.
type sessionState struct {
	id                int64
	started           bool
	lastExternalImage string
}

// killPreviousViewer best-effort kills whatever process opened the last
// externally-viewed image (the viewer command itself has already exited by
// the time we'd want to close it, so this matches on the temp file path in
// the target process's argv instead — catches viewers that take the path as
// a literal argument, e.g. feh/eog/sxiv, not browser- or portal-based
// handlers). No-op if nothing was opened yet.
func killPreviousViewer(sess *sessionState) {
	if sess.lastExternalImage == "" {
		return
	}
	_ = exec.Command("pkill", "-f", sess.lastExternalImage).Run()
	_ = os.Remove(sess.lastExternalImage)
	sess.lastExternalImage = ""
}

// endMCPSession closes out the session row on graceful server shutdown, if
// one was ever started. Uses a fresh context since ctx is typically already
// done by the time this runs. There's no "interrupted" case to distinguish
// here (unlike CLI practice) — this only runs on graceful shutdown; a crash
// or kill -9 just leaves exit_reason NULL, same as before this existed.
func endMCPSession(c *core.Core, sess *sessionState) {
	if !sess.started {
		return
	}
	if err := c.EndSession(context.Background(), sess.id, "completed"); err != nil {
		log.Printf("end session: %v", err)
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
