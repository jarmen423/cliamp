package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"rsc.io/qr"

	"github.com/bjarneo/cliamp/ipc"
)

func TestTokenLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", tokenFileName)
	if _, err := LoadToken(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadToken of a missing file error = %v, want fs.ErrNotExist", err)
	}
	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(first) {
		t.Fatalf("token %q is malformed", first)
	}
	again, err := LoadOrCreateToken(path)
	if err != nil || again != first {
		t.Fatalf("LoadOrCreateToken = %q, %v; want the saved %q", again, err, first)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Fatalf("token file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	rotated, err := RotateToken(path)
	if err != nil || rotated == first {
		t.Fatalf("RotateToken = %q, %v; want a new token", rotated, err)
	}
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateToken(path); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadOrCreateToken of a malformed token error = %v, want malformed", err)
	}
}

func TestTokenEqual(t *testing.T) {
	tests := []struct {
		presented, saved string
		want             bool
	}{
		{"abc", "abc", true},
		{"abd", "abc", false},
		{"ab", "abc", false},
		{"", "", false},
	}
	for _, tt := range tests {
		if got := tokenEqual(tt.presented, tt.saved); got != tt.want {
			t.Errorf("tokenEqual(%q, %q) = %v, want %v", tt.presented, tt.saved, got, tt.want)
		}
	}
}

// echoServer answers each V2 line with the line itself, which is enough to
// see the gateway carry one line per WebSocket message both ways.
type echoServer struct{}

func (echoServer) ServeConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if _, err := conn.Write(append(scanner.Bytes(), '\n')); err != nil {
			return
		}
	}
}

func newTestGateway(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, tokenFileName)
	token, err := RotateToken(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	artDir := filepath.Join(dir, "album-art")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := newGateway(Options{Server: echoServer{}, TokenPath: tokenPath, ArtDir: artDir})
	srv := httptest.NewServer(g.http.Handler)
	t.Cleanup(func() {
		g.cancel()
		srv.Close()
	})
	return srv, token, artDir
}

func TestGatewayAuthorization(t *testing.T) {
	srv, token, _ := newTestGateway(t)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v2"
	tests := []struct {
		name    string
		opts    *websocket.DialOptions
		wantErr bool
	}{
		{"no token", &websocket.DialOptions{Subprotocols: []string{Subprotocol}}, true},
		{"wrong token", &websocket.DialOptions{Subprotocols: []string{Subprotocol, tokenSubprotocolPrefix + strings.Repeat("0", 64)}}, true},
		{"subprotocol token", &websocket.DialOptions{Subprotocols: []string{Subprotocol, tokenSubprotocolPrefix + token}}, false},
		{"bearer header", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, resp, err := websocket.Dial(ctx, wsURL, tt.opts)
			if tt.wantErr {
				if err == nil {
					conn.CloseNow()
					t.Fatal("Dial succeeded, want 401")
				}
				if resp == nil || resp.StatusCode != http.StatusUnauthorized {
					t.Fatalf("Dial response = %v, want 401", resp)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if len(tt.opts.Subprotocols) > 0 && conn.Subprotocol() != Subprotocol {
				t.Errorf("Subprotocol = %q, want %q", conn.Subprotocol(), Subprotocol)
			}
			line := `{"version":2,"id":1,"method":"state.get"}`
			if err := conn.Write(ctx, websocket.MessageText, []byte(line+"\n")); err != nil {
				t.Fatal(err)
			}
			_, got, err := conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != line+"\n" {
				t.Errorf("message = %q, want one line %q", got, line)
			}
		})
	}
}

func TestGatewayArt(t *testing.T) {
	srv, token, artDir := newTestGateway(t)
	name := strings.Repeat("a", 64) + ".png"
	if err := os.WriteFile(filepath.Join(artDir, name), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(artDir), "secret.png"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{"cached art", "/art/" + name, token, http.StatusOK},
		{"no token", "/art/" + name, "", http.StatusUnauthorized},
		{"missing art", "/art/" + strings.Repeat("b", 64) + ".png", token, http.StatusNotFound},
		{"not a hash", "/art/secret.png", token, http.StatusNotFound},
		{"escape", "/art/..%2Fsecret.png", token, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

func TestGatewayServesAppWithoutToken(t *testing.T) {
	srv, _, _ := newTestGateway(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
}

func TestPairURLs(t *testing.T) {
	tests := []struct {
		listen    string
		wantURL   string
		wantLabel string
	}{
		{"100.101.102.103:7700", "http://100.101.102.103:7700/#t=tok", "Tailscale"},
		{"192.168.1.5:7700", "http://192.168.1.5:7700/#t=tok", "LAN"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000/#t=tok", "this computer"},
		{"box.local:7700", "http://box.local:7700/#t=tok", "LAN"},
	}
	for _, tt := range tests {
		urls, err := PairURLs(tt.listen, "tok")
		if err != nil {
			t.Fatal(err)
		}
		if len(urls) != 1 || urls[0].URL != tt.wantURL || urls[0].Label != tt.wantLabel {
			t.Errorf("PairURLs(%q) = %+v, want %s (%s)", tt.listen, urls, tt.wantURL, tt.wantLabel)
		}
	}
	if _, err := PairURLs("no-port", "tok"); err == nil {
		t.Error("PairURLs without a port succeeded")
	}
	urls, err := PairURLs(":7700", "tok")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if !strings.HasSuffix(u.URL, ":7700/#t=tok") {
			t.Errorf("PairURLs(:7700) URL = %q", u.URL)
		}
	}
}

func TestWriteQR(t *testing.T) {
	text := "http://192.168.1.5:7700/#t=" + strings.Repeat("ab", 32)
	var b strings.Builder
	if err := WriteQR(&b, text); err != nil {
		t.Fatal(err)
	}
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		t.Fatal(err)
	}
	// Read the half blocks back into modules: a lit half is a light module.
	const quiet = 2
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if want := (code.Size + 2*quiet + 1) / 2; len(lines) != want {
		t.Fatalf("QR has %d rows, want %d", len(lines), want)
	}
	for row, line := range lines {
		line = strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[97;40m"), "\x1b[0m")
		cells := []rune(line)
		if len(cells) != code.Size+2*quiet {
			t.Fatalf("row %d has %d cells, want %d", row, len(cells), code.Size+2*quiet)
		}
		for col, r := range cells {
			x, y := col-quiet, row*2-quiet
			top := r == '█' || r == '▀'
			bottom := r == '█' || r == '▄'
			if top != !code.Black(x, y) {
				t.Fatalf("module (%d,%d) light = %v, want %v", x, y, top, !code.Black(x, y))
			}
			if y+1 < code.Size+quiet && bottom != !code.Black(x, y+1) {
				t.Fatalf("module (%d,%d) light = %v, want %v", x, y+1, bottom, !code.Black(x, y+1))
			}
		}
	}
}

// TestGatewayCarriesIPC runs a real IPC server behind the gateway: a request
// gets its V2 response, and a subscription gets its acknowledgement and the
// events that the broker publishes.
func TestGatewayCarriesIPC(t *testing.T) {
	dir := t.TempDir()
	if len(dir) > 80 {
		dir, _ = os.MkdirTemp("/tmp", "c")
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	server, err := ipc.NewServer(filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	tokenPath := filepath.Join(dir, tokenFileName)
	token, err := RotateToken(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	g := newGateway(Options{Server: server, TokenPath: tokenPath})
	srv := httptest.NewServer(g.http.Handler)
	t.Cleanup(func() {
		g.cancel()
		srv.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dial := func() *websocket.Conn {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v2",
			&websocket.DialOptions{Subprotocols: []string{Subprotocol, tokenSubprotocolPrefix + token}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.CloseNow() })
		return conn
	}
	roundTrip := func(conn *websocket.Conn, line string) map[string]any {
		t.Helper()
		if line != "" {
			if err := conn.Write(ctx, websocket.MessageText, []byte(line+"\n")); err != nil {
				t.Fatal(err)
			}
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("message %q: %v", data, err)
		}
		return out
	}

	requests := dial()
	if got := roundTrip(requests, `{"version":2,"id":"c","method":"capabilities"}`); got["ok"] != true || got["id"] != "c" {
		t.Fatalf("capabilities = %v", got)
	}

	events := dial()
	if got := roundTrip(events, `{"version":2,"id":"e","method":"subscribe","topics":["runtime.state"]}`); got["ok"] != true {
		t.Fatalf("subscribe = %v", got)
	}
	// The acknowledgement goes out after the subscription registers.
	if err := server.Broker().Publish("runtime.state", json.RawMessage(`{"state":"playing"}`), false); err != nil {
		t.Fatal(err)
	}
	if got := roundTrip(events, ""); got["event"] != "runtime.state" {
		t.Fatalf("event = %v", got)
	}
}

func TestVirtualBridge(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"docker0", true},
		{"br-3f2a9c", true},
		{"virbr0", true},
		{"veth12ab", true},
		{"wlp0s20f3", false},
		{"eth0", false},
		{"tailscale0", false},
		{"enp3s0", false},
	}
	for _, tt := range tests {
		if got := virtualBridge(tt.name); got != tt.want {
			t.Errorf("virtualBridge(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBasePairURL(t *testing.T) {
	tests := []struct {
		base      string
		wantURL   string
		wantLabel string
		wantErr   bool
	}{
		{"https://box.tail1.ts.net:7443", "https://box.tail1.ts.net:7443/#t=tok", "HTTPS", false},
		{"https://box.tail1.ts.net/", "https://box.tail1.ts.net/#t=tok", "HTTPS", false},
		{"http://192.168.1.5:7700", "http://192.168.1.5:7700/#t=tok", "proxy", false},
		{"box.tail1.ts.net", "", "", true},
		{"ftp://box", "", "", true},
		{"https://box/cliamp", "", "", true},
		{"https://box/?x=1", "", "", true},
	}
	for _, tt := range tests {
		got, err := BasePairURL(tt.base, "tok")
		if (err != nil) != tt.wantErr {
			t.Errorf("BasePairURL(%q) error = %v, wantErr %v", tt.base, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && (got.URL != tt.wantURL || got.Label != tt.wantLabel) {
			t.Errorf("BasePairURL(%q) = %+v, want %s (%s)", tt.base, got, tt.wantURL, tt.wantLabel)
		}
	}
}
