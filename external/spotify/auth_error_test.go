package spotify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"github.com/devgianlu/go-librespot/audio"

	"github.com/bjarneo/cliamp/playlist"
)

// TestIsAuthError pins down which errors trigger Spotify re-authentication.
// Rapid track skipping cancels in-flight stream creation, surfacing
// context.DeadlineExceeded / context.Canceled — these MUST NOT be treated as
// auth errors, otherwise the streamer escalates to a browser re-auth flow
// even though the session is healthy. See the regression discussion in
// fix/spotify-rapid-skip-reauth.
func TestIsAuthError(t *testing.T) {
	wrappedDeadline := fmt.Errorf("librespot: fetch chunk: %w", context.DeadlineExceeded)
	wrappedCanceled := fmt.Errorf("librespot: fetch chunk: %w", context.Canceled)
	keyErr := &audio.KeyProviderError{Code: 1}
	wrappedKeyErr := fmt.Errorf("spotify: %w", keyErr)

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context.DeadlineExceeded (skip cancellation)", context.DeadlineExceeded, false},
		{"wrapped DeadlineExceeded", wrappedDeadline, false},
		{"context.Canceled (skip cancellation)", context.Canceled, false},
		{"wrapped Canceled", wrappedCanceled, false},
		{"plain network error", errors.New("connection reset by peer"), false},
		{"KeyProviderError (real auth signal)", keyErr, true},
		{"wrapped KeyProviderError", wrappedKeyErr, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsConnDropped pins down which stream errors get one retry: a key request
// written to an access point connection that just dropped (the error seen on a
// flaky network), but not timeouts, cancellations, refused keys, or a closed
// access point, which have their own handling.
func TestIsConnDropped(t *testing.T) {
	writeErr := &net.OpError{Op: "write", Net: "tcp", Err: errors.New("wsasend: An existing connection was forcibly closed by the remote host.")}
	sendFailed := fmt.Errorf("failed retrieving audio key: %w",
		fmt.Errorf("failed sending key request for file a21d, gid: x: %w", fmt.Errorf("failed writing packet: %w", writeErr)))

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"key request on a dropped connection", sendFailed, true},
		{"read EOF", fmt.Errorf("failed retrieving audio key: %w", io.EOF), true},
		{"closed conn", fmt.Errorf("x: %w", net.ErrClosed), true},
		{"timeout", fmt.Errorf("failed retrieving audio key: %w", context.DeadlineExceeded), false},
		{"canceled", context.Canceled, false},
		{"refused key", &audio.KeyProviderError{Code: 1}, false},
		{"closed access point", fmt.Errorf("x: %w", ap.ErrAccesspointClosed), false},
		{"other", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnDropped(tt.err); got != tt.want {
				t.Fatalf("isConnDropped(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestClosedAccesspointIsSessionLost drives go-librespot's real key provider
// over a closed access point, the state it leaves after giving up on a dropped
// connection, so a library change to that error breaks this test rather than
// leaving playback dead until restart.
func TestClosedAccesspointIsSessionLost(t *testing.T) {
	accesspoint := ap.NewAccesspoint(&librespot.NullLogger{}, func(context.Context) string { return "127.0.0.1:1" }, "test-device")
	accesspoint.Close()
	keys := audio.NewAudioKeyProvider(&librespot.NullLogger{}, accesspoint)

	_, err := keys.Request(context.Background(), make([]byte, 16), make([]byte, 20))
	if err == nil {
		t.Fatal("key request on a closed access point succeeded")
	}
	// Wrapped the way go-librespot's player.NewStream wraps it.
	err = fmt.Errorf("failed retrieving audio key: %w", err)
	if !isSessionLost(err) {
		t.Fatalf("isSessionLost(%v) = false, want true", err)
	}
	if isSessionLost(fmt.Errorf("failed retrieving audio key: %w", context.DeadlineExceeded)) {
		t.Fatal("a key request timeout must not rebuild the session")
	}
}

// TestErrAfterReconnect pins down the error a stream failure gets once a
// silent reconnect succeeded. A refused audio key then means Spotify refuses
// the track for the account, so it must not ask for sign-in again.
func TestErrAfterReconnect(t *testing.T) {
	keyErr := fmt.Errorf("failed retrieving audio key: %w", &audio.KeyProviderError{Code: 1})
	tests := []struct {
		name        string
		err         error
		wantRefused bool
	}{
		{"refused audio key", keyErr, true},
		{"network error", errors.New("connection reset by peer"), false},
		{"timeout", fmt.Errorf("fetch chunk: %w", context.DeadlineExceeded), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errAfterReconnect(tt.err)
			if errors.Is(got, playlist.ErrNeedsAuth) {
				t.Fatalf("errAfterReconnect(%v) = %v, want no ErrNeedsAuth after a successful reconnect", tt.err, got)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("errAfterReconnect(%v) = %v, want it to wrap the stream error", tt.err, got)
			}
			var refused *playlist.StreamRefusedError
			if isRefused := errors.As(got, &refused); isRefused != tt.wantRefused {
				t.Fatalf("errAfterReconnect(%v) is StreamRefusedError = %v, want %v", tt.err, isRefused, tt.wantRefused)
			}
		})
	}
}

// TestKeyRefusedRecently checks that refusals soon after a reconnect that did
// not help skip the next reconnect, and later ones try again.
func TestKeyRefusedRecently(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		keyRefusedAt time.Time
		want         bool
	}{
		{"never refused", time.Time{}, false},
		{"refused just now", now.Add(-time.Second), true},
		{"refused at the window edge", now.Add(-keyRefusedWindow), false},
		{"refused long ago", now.Add(-time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &SpotifyProvider{keyRefusedAt: tt.keyRefusedAt}
			if got := p.keyRefusedRecently(now); got != tt.want {
				t.Fatalf("keyRefusedRecently() = %v, want %v", got, tt.want)
			}
		})
	}
}
