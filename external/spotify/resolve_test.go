package spotify

import (
	"testing"

	librespot "github.com/devgianlu/go-librespot"
	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/bjarneo/cliamp/playlist"
)

func TestTrackFromMetadata(t *testing.T) {
	gid := func(b byte) []byte {
		g := make([]byte, 16)
		g[15] = b
		return g
	}
	small := metadatapb.Image_SMALL
	def := metadatapb.Image_DEFAULT
	tr := &metadatapb.Track{
		Name:     proto.String("Ain't In LA"),
		Duration: proto.Int32(184927),
		Number:   proto.Int32(3),
		Artist: []*metadatapb.Artist{
			{Gid: gid(1), Name: proto.String("ADÉLA")},
			{Gid: gid(2), Name: proto.String("Guest")},
		},
		Album: &metadatapb.Album{
			Gid:  gid(9),
			Name: proto.String("Ain't In LA"),
			Date: &metadatapb.Date{Year: proto.Int32(2026)},
			CoverGroup: &metadatapb.ImageGroup{Image: []*metadatapb.Image{
				{FileId: []byte{0xab, 0x01}, Size: &small},
				{FileId: []byte{0xab, 0x02}, Size: &def},
			}},
		},
	}

	got := trackFromMetadata("spotify:track:x", tr)
	artistIDs := librespot.SpotifyIdFromGid(librespot.SpotifyIdTypeArtist, gid(1)).Base62() + "," +
		librespot.SpotifyIdFromGid(librespot.SpotifyIdTypeArtist, gid(2)).Base62()
	want := playlist.Track{
		Path:         "spotify:track:x",
		Title:        "Ain't In LA",
		Artist:       "ADÉLA, Guest",
		Album:        "Ain't In LA",
		AlbumArtURL:  "https://i.scdn.co/image/ab02",
		Year:         2026,
		DurationSecs: 184,
		TrackNumber:  3,
	}
	if got.Path != want.Path || got.Title != want.Title || got.Artist != want.Artist || got.Album != want.Album ||
		got.AlbumArtURL != want.AlbumArtURL || got.Year != want.Year || got.DurationSecs != want.DurationSecs ||
		got.TrackNumber != want.TrackNumber {
		t.Fatalf("track = %+v\nwant %+v", got, want)
	}
	if got.ProviderMeta[metaSpotifyArtistIDs] != artistIDs {
		t.Errorf("artist IDs = %q, want %q", got.ProviderMeta[metaSpotifyArtistIDs], artistIDs)
	}
	if got.AlbumID() == "" {
		t.Error("album ID missing; album drill-down from the track needs it")
	}
}

func TestEpisodeFromMetadataUsesShow(t *testing.T) {
	ep := &metadatapb.Episode{
		Name:     proto.String("Ep 1"),
		Duration: proto.Int32(60000),
		Show: &metadatapb.Show{
			Name:       proto.String("The Show"),
			CoverImage: &metadatapb.ImageGroup{Image: []*metadatapb.Image{{FileId: []byte{0x01}}}},
		},
	}
	got := episodeFromMetadata("spotify:episode:e", ep)
	if got.Artist != "The Show" || got.Album != "The Show" || got.AlbumArtURL != "https://i.scdn.co/image/01" || got.DurationSecs != 60 {
		t.Fatalf("episode = %+v", got)
	}
}
