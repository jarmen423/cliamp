package spotify

import (
	"context"
	"fmt"
	"slices"
	"testing"

	librespot "github.com/devgianlu/go-librespot"
	connectpb "github.com/devgianlu/go-librespot/proto/spotify/connectstate"
	"github.com/devgianlu/go-librespot/tracks"
)

// testContextList builds a playlist context whose pages are all inline, so
// the resolver never goes to the network. Track n is spotify:track:tN.
func testContextList(t *testing.T, pageSizes ...int) *tracks.List {
	t.Helper()
	spotCtx := &connectpb.Context{Uri: "spotify:playlist:test"}
	n := 0
	for _, size := range pageSizes {
		page := &connectpb.ContextPage{}
		for range size {
			page.Tracks = append(page.Tracks, &connectpb.ContextTrack{Uri: fmt.Sprintf("spotify:track:t%d", n)})
			n++
		}
		spotCtx.Pages = append(spotCtx.Pages, page)
	}
	tl, err := tracks.NewTrackListFromContext(context.Background(), &librespot.NullLogger{}, nil, spotCtx)
	if err != nil {
		t.Fatalf("NewTrackListFromContext: %v", err)
	}
	return tl
}

func trackURIs(from, to int) []string {
	var uris []string
	for i := from; i < to; i++ {
		uris = append(uris, fmt.Sprintf("spotify:track:t%d", i))
	}
	return uris
}

func TestAllContextTracks(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name      string
		pageSizes []int
		seekTo    string // track the list is positioned on first, as cmdTransfer does
		ctx       context.Context
		limit     int
		want      []string
	}{
		{
			name:      "every page from the start",
			pageSizes: []int{2, 3, 1},
			limit:     100,
			want:      trackURIs(0, 6),
		},
		{
			// Seeking loads only the pages up to the target, so the pages
			// after it must still be fetched.
			name:      "pages after a seek target",
			pageSizes: []int{2, 3, 4},
			seekTo:    "spotify:track:t3",
			limit:     100,
			want:      trackURIs(0, 9),
		},
		{
			name:      "track outside the context comes first",
			pageSizes: []int{2, 2},
			seekTo:    "spotify:track:queued",
			limit:     100,
			want:      append([]string{"spotify:track:queued"}, trackURIs(0, 4)...),
		},
		{
			name:      "limit cuts the walk",
			pageSizes: []int{3, 3, 3},
			limit:     5,
			want:      trackURIs(0, 5),
		},
		{
			name:      "cancelled ctx stops after the first track",
			pageSizes: []int{2, 2},
			ctx:       cancelled,
			limit:     100,
			want:      trackURIs(0, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tl := testContextList(t, tt.pageSizes...)
			if tt.seekTo != "" {
				if err := tl.TrySeekTo(context.Background(), &connectpb.ContextTrack{Uri: tt.seekTo}); err != nil {
					t.Fatalf("TrySeekTo: %v", err)
				}
			}
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			var got []string
			for _, pt := range allContextTracks(ctx, tl, tt.limit) {
				got = append(got, pt.Uri)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
