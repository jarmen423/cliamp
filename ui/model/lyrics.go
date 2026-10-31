package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/lyrics"
	"github.com/bjarneo/cliamp/playlist"
	"github.com/bjarneo/cliamp/ui"
)

// trackLyricsSource is a provider that has synced lyrics for its own tracks,
// as the Spotify provider has. It returns lyrics.ErrNotFound at once for a
// track that is not its own.
type trackLyricsSource interface {
	TrackLyrics(ctx context.Context, track playlist.Track) ([]lyrics.Line, error)
}

// trackLyricsSources returns the registered providers that have synced
// lyrics, in provider order.
func (m Model) trackLyricsSources() []trackLyricsSource {
	var sources []trackLyricsSource
	for _, entry := range m.providers {
		if s, ok := entry.Provider.(trackLyricsSource); ok {
			sources = append(sources, s)
		}
	}
	return sources
}

// lyricsLookups returns the lyrics lookups of sources for track, in the
// order of sources.
func lyricsLookups(track playlist.Track, sources []trackLyricsSource) []lyrics.Source {
	lookups := make([]lyrics.Source, 0, len(sources))
	for _, source := range sources {
		lookups = append(lookups, func(ctx context.Context) ([]lyrics.Line, error) {
			return source.TrackLyrics(ctx, track)
		})
	}
	return lookups
}

// lyricsArtistTitle resolves the best artist and title for a lyrics lookup.
// For streams with ICY metadata ("Artist - Song"), it parses the stream title.
// For regular tracks, it uses the track's metadata fields.
func (m *Model) lyricsArtistTitle() (artist, title string) {
	track, idx := m.currentPlaybackTrack()
	if idx < 0 {
		return "", ""
	}
	// For streams, prefer the live ICY stream title which updates per-song.
	if m.streamTitle != "" && track.Stream {
		if a, t, ok := splitStreamTitle(m.streamTitle); ok {
			return a, t
		}
	}
	return track.Artist, track.Title
}

func lyricsLookupKey(track playlist.Track, artist, title string) string {
	if artist != "" && title != "" {
		return artist + "\n" + title
	}
	if track.EmbeddedLyrics == "" {
		return ""
	}
	if track.Path != "" {
		return "embedded\n" + track.Path
	}
	if track.Title != "" {
		return "embedded\n" + track.Title
	}
	return "embedded"
}

// lyricsWanted reports whether a view that shows lyrics is open, so lookups
// run. Update calls ensureLyrics when this turns true.
func (m Model) lyricsWanted() bool {
	return m.lyrics.visible ||
		(m.immersive.active && m.immersive.view == immViewLyrics) ||
		(m.fullVis && m.lyrics.fullVis != fullVisLyricsOff)
}

// ensureLyrics starts a lookup for the playing track when a lyrics view is
// open and the last lookup for it neither found lyrics nor found that it has
// none. A failed lookup is tried again.
func (m *Model) ensureLyrics() tea.Cmd {
	if !m.lyricsWanted() {
		return nil
	}
	track, _ := m.currentPlaybackTrack()
	artist, title := m.lyricsArtistTitle()
	q := lyricsLookupKey(track, artist, title)
	if q == m.lyrics.query && (len(m.lyrics.lines) > 0 || errors.Is(m.lyrics.err, lyrics.ErrNotFound)) {
		return nil
	}
	return m.retryLyrics()
}

func (m *Model) retryLyrics() tea.Cmd {
	if m.lyrics.loading {
		return nil
	}
	track, _ := m.currentPlaybackTrack()
	artist, title := m.lyricsArtistTitle()
	q := lyricsLookupKey(track, artist, title)
	if q == "" {
		return nil
	}
	m.lyrics.query = q
	m.lyrics.loading = true
	m.lyrics.lines = nil
	m.lyrics.err = nil
	return m.fetchLyricsForTrack(track, artist, title)
}

// lyricsSyncable reports whether synced lyrics can track the current playback
// position. This is true for local files, Navidrome streams (which have
// accurate position tracking), and yt-dlp tracks (whose ytdlPipeStreamer
// reports position from decoded PCM frames). It is false for live radio (ICY —
// position is from stream start, not song start) and for live streams with no
// finite duration, where the position doesn't map to song time.
func (m *Model) lyricsSyncable() bool {
	track, idx := m.currentPlaybackTrack()
	if idx < 0 {
		return false
	}
	// Live-stream position is not song-relative, regardless of provider metadata.
	if m.currentPlaybackIsLive(track) {
		return false
	}
	// yt-dlp pipe streams track position from decoded frames, so synced lyrics
	// can follow them. Exclude streams without a known duration (e.g. YouTube
	// Live), where the position is not relative to the song.
	if playlist.IsYTDL(track.Path) {
		return track.DurationSecs > 0
	}
	// For unclassified streams, retain the conservative fallback: bare HTTP
	// streams may be radio, while on-demand providers supply track metadata.
	if track.Stream && len(track.ProviderMeta) == 0 {
		return false
	}
	return true
}

// lyricsHaveTimestamps reports whether the loaded lyrics have meaningful
// timestamps (i.e., not all lines at 0).
func (m *Model) lyricsHaveTimestamps() bool {
	for _, l := range m.lyrics.lines {
		if l.Start > 0 {
			return true
		}
	}
	return false
}

// lyricsFollowPlayback reports whether the lyrics views follow the playback
// position, rather than scroll by hand.
func (m *Model) lyricsFollowPlayback() bool {
	return m.lyricsSyncable() && m.lyricsHaveTimestamps()
}

// scrollLyrics moves lyrics that do not follow playback by delta lines.
func (m *Model) scrollLyrics(delta int) {
	if !m.lyricsFollowPlayback() {
		m.lyrics.scroll = clampInt(m.lyrics.scroll+delta, 0, max(len(m.lyrics.lines)-1, 0))
	}
}

// lyricsPaneMargin is the indent of every row of a lyrics pane.
const lyricsPaneMargin = "  "

// lyricsPane returns at most rows lines of the lyrics state for a pane w cells
// wide: the lookup's status, or the lyrics wrapped to the pane. Lyrics that
// follow playback keep the active line near the middle.
func (m Model) lyricsPane(w, rows int) []string {
	if rows <= 0 {
		return nil
	}
	textW := max(1, w-len(lyricsPaneMargin))
	var lines []string
	switch {
	case m.lyrics.loading || m.lyrics.err != nil || len(m.lyrics.lines) == 0:
		lines = m.lyricsStatusRows()
	case m.lyricsFollowPlayback():
		lines = m.syncedLyricsRows(textW, rows)
	default:
		for i := m.lyrics.scroll; i < len(m.lyrics.lines) && len(lines) < rows; i++ {
			lines = append(lines, lyricsLineRows(m.lyrics.lines[i].Text, textW, dimStyle)...)
		}
	}
	return lines[:min(rows, len(lines))]
}

// lyricsStatusRows says why a lyrics pane has no lyrics to show.
func (m Model) lyricsStatusRows() []string {
	dim := func(s string) string { return dimStyle.Render(lyricsPaneMargin + s) }
	switch {
	case m.lyrics.loading:
		return []string{loadingLine("Searching for lyrics...")}
	case errors.Is(m.lyrics.err, lyrics.ErrNotFound):
		return []string{dim("No lyrics found for this track.")}
	case m.lyrics.err != nil:
		return []string{errorStyle.Render(lyricsPaneMargin + "Lyrics fetch failed: " + m.lyrics.err.Error())}
	}
	if artist, title := m.lyricsArtistTitle(); artist == "" && title == "" {
		rows := []string{dim("No artist/title metadata available.")}
		if track, idx := m.currentPlaybackTrack(); idx >= 0 && track.Stream {
			rows = append(rows, dim("Waiting for stream metadata..."))
		}
		return rows
	}
	if m.lyrics.visible {
		return []string{dim("No lyrics loaded. Press r to retry.")}
	}
	return []string{dim("No lyrics loaded.")}
}

// syncedLyricsRows lays out the lyrics around the active line, in at least
// rows lines when the lyrics have them. It wraps only the lines it can show,
// since it runs every frame.
func (m Model) syncedLyricsRows(textW, rows int) []string {
	all := m.lyrics.lines
	pos := m.lyricsPlaybackPosition()
	active := -1
	for i, line := range all {
		if line.Start > pos {
			break
		}
		active = i
	}
	var above, cur, below []string
	if active >= 0 {
		cur = lyricsLineRows(all[active].Text, textW, playlistSelectedStyle)
	}
	next := active + 1
	fillBelow := func(limit int) {
		for ; next < len(all) && len(above)+len(cur)+len(below) < limit; next++ {
			below = append(below, lyricsLineRows(all[next].Text, textW, dimStyle)...)
		}
	}
	// Rows above the active line get half the pane, and whatever the lines
	// below cannot fill at the end of the track.
	fillBelow(rows - rows/2)
	budget := max(0, rows-len(cur)-len(below))
	for i := active - 1; i >= 0 && len(above) < budget; i-- {
		above = append(lyricsLineRows(all[i].Text, textW, dimStyle), above...)
	}
	if len(above) > budget {
		above = above[len(above)-budget:]
	}
	// At the start of the track the lines below take the rows left over.
	fillBelow(rows)
	return append(append(above, cur...), below...)
}

// lyricsLineRows wraps one lyric line to textW cells and styles each row. An
// empty line, an instrumental break, shows as a note.
func lyricsLineRows(text string, textW int, style lipgloss.Style) []string {
	if text == "" {
		text = "♪"
	}
	wrapped := strings.Split(ansi.Wrap(text, textW, ""), "\n")
	for i, row := range wrapped {
		wrapped[i] = style.Render(lyricsPaneMargin + row)
	}
	return wrapped
}

// fullVisLyricsMode is how the full-screen visualizer shows lyrics.
type fullVisLyricsMode int

const (
	fullVisLyricsOff   fullVisLyricsMode = iota
	fullVisLyricsSplit                   // visualizer on the left, lyrics on the right
	fullVisLyricsOnly                    // lyrics in place of the visualizer
	fullVisLyricsCount
)

// fullVisSplitMinWidth is the narrowest panel that splits. Below it the
// lyrics take the whole screen.
const fullVisSplitMinWidth = 70

// fullVisSplitGutter separates the split's visualizer and lyrics.
const fullVisSplitGutter = 2

// fullVisLyricsNow is the lyrics mode the current layout can show: a split
// with no room or no visualizer shows the lyrics alone.
func (m Model) fullVisLyricsNow() fullVisLyricsMode {
	if m.lyrics.fullVis == fullVisLyricsSplit && (ui.PanelWidth < fullVisSplitMinWidth || m.visualizerDisabled()) {
		return fullVisLyricsOnly
	}
	return m.lyrics.fullVis
}

// fullVisWidth is the width of the full-screen visualizer in cells.
func (m Model) fullVisWidth() int {
	if m.fullVisLyricsNow() == fullVisLyricsSplit {
		return ui.PanelWidth * 3 / 5
	}
	return ui.PanelWidth
}

// cycleFullVisLyrics is y in the full-screen visualizer: visualizer, split,
// lyrics only, and back.
func (m *Model) cycleFullVisLyrics() {
	m.lyrics.fullVis = (m.fullVisLyricsNow() + 1) % fullVisLyricsCount
	m.recomputeLayout()
}

// handleFullVisLyricsKey runs the lyrics keys of the full-screen visualizer
// while it shows lyrics, and reports whether key was one of them.
func (m *Model) handleFullVisLyricsKey(key string) bool {
	if m.fullVisLyricsNow() == fullVisLyricsOff {
		return false
	}
	switch key {
	case "up", "k":
		m.scrollLyrics(-1)
	case "down", "j":
		m.scrollLyrics(1)
	case "[":
		m.nudgeSyncedLyrics(-lyricsOffsetStep)
	case "]":
		m.nudgeSyncedLyrics(lyricsOffsetStep)
	default:
		return false
	}
	return true
}

// lyricsOffsetStep is one press of [ or ].
const lyricsOffsetStep = 250 * time.Millisecond

// nudgeSyncedLyrics adjusts the offset of lyrics that follow playback.
func (m *Model) nudgeSyncedLyrics(delta time.Duration) {
	if m.lyricsFollowPlayback() {
		m.nudgeLyricsOffset(delta)
	}
}

// maxLyricsOffset bounds the user-adjustable synced-lyrics drift correction.
const maxLyricsOffset = 10 * time.Second

// lyricsPlaybackPosition returns the playback position adjusted by the
// user's synced-lyrics offset. A positive offset advances the position, so
// a later lyric line becomes active sooner. It reads the position cached at
// each tick, since View calls it every frame.
func (m Model) lyricsPlaybackPosition() time.Duration {
	return m.cachedPos + m.lyrics.offset
}

// SetLyricsOffset loads a persisted lyric timestamp offset (ms) at startup.
func (m *Model) SetLyricsOffset(ms int) {
	d := time.Duration(ms) * time.Millisecond
	if d > maxLyricsOffset {
		d = maxLyricsOffset
	}
	if d < -maxLyricsOffset {
		d = -maxLyricsOffset
	}
	m.lyrics.offset = d
}

// nudgeLyricsOffset shifts the synced-lyrics position by delta and persists
// the result. A positive offset advances the active lyric line, correcting
// timestamps that run late; a negative offset delays it. Spotify and
// Musixmatch timestamps are often offset from the master by a constant
// amount per track.
func (m *Model) nudgeLyricsOffset(delta time.Duration) {
	offset := m.lyrics.offset + delta
	if offset > maxLyricsOffset {
		offset = maxLyricsOffset
	}
	if offset < -maxLyricsOffset {
		offset = -maxLyricsOffset
	}
	m.lyrics.offset = offset
	m.status.Warningf(statusTTLDefault, "Lyrics offset: %s", formatLyricsOffset(offset))
	_ = m.saveConfigFloat("lyrics_offset_ms", float64(offset.Milliseconds()), 0)
}

// formatLyricsOffset renders a lyric offset with an explicit sign, e.g. "+0.5s".
func formatLyricsOffset(d time.Duration) string {
	ms := d.Milliseconds()
	sign := "+"
	if ms < 0 {
		sign = "-"
		ms = -ms
	}
	return fmt.Sprintf("%s%.1fs", sign, float64(ms)/1000)
}
