// Package remote serves the cliamp remote web app and the V2 IPC protocol
// over the network, so a phone can control a running cliamp.
//
// The gateway adds no protocol of its own. A WebSocket on /v2 carries the
// same newline-delimited V2 JSON as the Unix socket, one line per message,
// and the IPC server handles it as it handles a socket connection. Every
// request to /v2 and /art must carry the pairing token, as an
// "Authorization: Bearer" header or, from a browser, as the WebSocket
// subprotocol "cliamp.token.<token>".
package remote

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/bjarneo/cliamp/applog"
)

//go:embed web
var webFiles embed.FS

func init() {
	// The gateway sends nosniff, so the manifest needs its own type.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Subprotocol is the WebSocket subprotocol of the V2 stream. A browser
// offers it together with the token subprotocol.
const Subprotocol = "cliamp.v2"

// tokenSubprotocolPrefix starts the subprotocol that carries the token from
// a browser, which cannot set headers on a WebSocket.
const tokenSubprotocolPrefix = "cliamp.token."

// maxConns bounds the open WebSockets. A client uses two: one for requests
// and one for events.
const maxConns = 16

// artName matches the file names in the album-art cache.
var artName = regexp.MustCompile(`^[0-9a-f]{64}\.(jpg|png|gif|webp|bmp|tiff)$`)

// ConnServer serves V2 requests from a connection. *ipc.Server implements it.
type ConnServer interface {
	ServeConn(net.Conn)
}

// Options configures a Gateway.
type Options struct {
	Addr      string     // host:port to listen on
	Server    ConnServer // serves the V2 stream of each WebSocket
	TokenPath string     // the pairing token file, read for each request
	ArtDir    string     // the album-art cache directory; empty serves no art
}

// Gateway is a running remote gateway.
type Gateway struct {
	opts     Options
	listener net.Listener
	http     *http.Server
	ctx      context.Context
	cancel   context.CancelFunc
	slots    chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

// Start listens on opts.Addr and serves until Close.
func Start(opts Options) (*Gateway, error) {
	if opts.Server == nil {
		return nil, errors.New("remote: no V2 server")
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("remote: listen: %w", err)
	}
	g := newGateway(opts)
	g.listener = ln
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := g.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			applog.Warn("remote: serve: %v", err)
		}
	}()
	applog.Info("remote: listening on %s", ln.Addr())
	return g, nil
}

func newGateway(opts Options) *Gateway {
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		opts:   opts,
		ctx:    ctx,
		cancel: cancel,
		slots:  make(chan struct{}, maxConns),
	}
	g.http = &http.Server{
		Handler:           g.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	return g
}

// Addr returns the address the gateway listens on.
func (g *Gateway) Addr() net.Addr { return g.listener.Addr() }

// Close stops the gateway and closes every WebSocket.
func (g *Gateway) Close() error {
	var err error
	g.once.Do(func() {
		// Canceling the context ends the hijacked WebSockets, which
		// http.Server.Close does not track.
		g.cancel()
		err = g.http.Close()
		g.wg.Wait()
	})
	return err
}

func (g *Gateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2", g.serveV2)
	mux.HandleFunc("GET /art/{name}", g.serveArt)
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServerFS(static))
	return securityHeaders(mux)
}

// securityHeaders keeps the web app from being framed or loading code from
// anywhere but the gateway. Album art loads from the providers over https.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: blob: data:; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The app is small and changes with cliamp, so a client always
		// revalidates it.
		h.Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// authorized reports whether r carries the saved token. It reads the token
// file each time, so cliamp remote pair --rotate takes effect at once.
func (g *Gateway) authorized(r *http.Request) bool {
	saved, err := LoadToken(g.opts.TokenPath)
	if err != nil {
		applog.Warn("remote: %v", err)
		return false
	}
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return tokenEqual(strings.TrimSpace(bearer), saved)
	}
	for _, protocol := range requestedSubprotocols(r) {
		if token, ok := strings.CutPrefix(protocol, tokenSubprotocolPrefix); ok {
			return tokenEqual(token, saved)
		}
	}
	return false
}

// requestedSubprotocols returns the subprotocols a WebSocket request offers.
func requestedSubprotocols(r *http.Request) []string {
	var out []string
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for _, p := range strings.Split(header, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func (g *Gateway) serveV2(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{Subprotocol},
		// The token, not the origin, authorizes a client. A page on
		// another site cannot read the token, and a native app sends no
		// browser origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return // Accept has written the error response.
	}
	g.opts.Server.ServeConn(websocket.NetConn(g.ctx, conn, websocket.MessageText))
}

func (g *Gateway) serveArt(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	name := r.PathValue("name")
	if g.opts.ArtDir == "" || !artName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	// The name is a content hash, so a cached copy never goes stale.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(g.opts.ArtDir, name))
}
