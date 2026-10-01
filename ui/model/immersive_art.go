package model

// immersive_art.go puts real cover art into the immersive frame's art boxes
// (Now Playing and the rows/grid canvas thumbnails). Covers are fetched and
// decoded once per URL, then encoded once per box size: as kitty graphics
// images shown through Unicode placeholder cells (plain text, no output
// layer), as Sixel images drawn through the termimg layer on top of blank
// cells, or as truecolor half-block text everywhere else. Until a cover is
// ready the box keeps its text placeholder.

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // register decoders for cover art
	_ "image/jpeg"
	_ "image/png"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/bjarneo/cliamp/applog"
	"github.com/bjarneo/cliamp/internal/httpclient"
	"github.com/bjarneo/cliamp/ui/termimg"
)

// imageMode is the "images" config key.
type imageMode int

const (
	imagesAuto   imageMode = iota // kitty graphics or Sixel when the terminal supports it, else blocks
	imagesSixel                   // always Sixel
	imagesBlocks                  // always half-block text
	imagesOff                     // text placeholders only
	imagesKitty                   // always kitty graphics (Unicode placeholders)
)

// artKind is how a cover is drawn right now.
type artKind int

const (
	artNone artKind = iota
	artSixel
	artBlocks
	artKitty
)

const (
	artMaxBytes     = 8 << 20 // covers larger than this are not decoded
	artFetchTimeout = 15 * time.Second
	artPollEvery    = 250 * time.Millisecond
	artMaxImages    = 128 // decoded covers kept before the cache is reset
	artMaxEncodings = 512 // encoded covers kept before the cache is reset
	artMaxInflight  = 6   // concurrent fetches
	artMaxSide      = 640 // decoded covers are downscaled to this; boxes are far smaller
	artMaxKitty     = 64  // kitty images kept in the terminal before off-screen ones are deleted
)

// kittyVisID is the pixel visualizer's kitty image; covers never use it.
var kittyVisID = termimg.KittyID(termimg.KittyIDs - 1)

// artStore caches decoded covers by URL and their encodings by box size.
// It is shared by pointer so View (on a Model copy) and Update agree.
type artStore struct {
	mu       sync.Mutex
	imgs     map[string]*artImage
	encs     map[artKey]*artEncoding
	inflight int
	kittySeq int // next kitty image ID sequence number
}

type artImage struct {
	img     image.Image
	loading bool
	failed  bool
}

type artKey struct {
	url   string
	w, h  int // cells
	cellW int // pixels per cell when encoded (Sixel sizes depend on it)
	cellH int
	kind  artKind
}

type artEncoding struct {
	sixel   []byte
	blocks  []string
	kittyID uint32   // kitty image ID, assigned when the encode starts
	kitty   []string // placeholder lines, set once the image is transmitted
	loading bool
}

func newArtStore() *artStore {
	return &artStore{imgs: map[string]*artImage{}, encs: map[artKey]*artEncoding{}}
}

// SetImageMode applies the "images" config key.
func (m *Model) SetImageMode(s string) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sixel":
		m.imgMode = imagesSixel
	case "kitty":
		m.imgMode = imagesKitty
	case "blocks", "halfblocks", "text":
		m.imgMode = imagesBlocks
	case "off", "none", "false":
		m.imgMode = imagesOff
	default:
		m.imgMode = imagesAuto
	}
	m.imgTmux = os.Getenv("TMUX") != ""
	m.kittyNamed = kittyPlaceholderEnv()
}

// SetImageLayer attaches the output layer images are drawn through.
func (m *Model) SetImageLayer(l *termimg.Layer) {
	m.imgLayer = l
	if m.art == nil {
		m.art = newArtStore()
	}
	if m.pixVis == nil && l != nil {
		m.pixVis = newPixVisWorker(l)
	}
	if m.frameMemo == nil {
		m.frameMemo = &frameMemo{}
	}
}

// SetImageOutput sets where kitty graphics sequences are written: the
// program output, so they are serialized with Bubbletea's frames. Without
// one (tests) kitty images are never transmitted.
func (m *Model) SetImageOutput(w io.Writer) { m.imgOut = w }

// artKindNow resolves the image mode against what the terminal reported.
func (m Model) artKindNow() artKind {
	if m.art == nil {
		return artNone
	}
	switch m.imgMode {
	case imagesOff:
		return artNone
	case imagesSixel:
		return artSixel
	case imagesBlocks:
		return artBlocks
	case imagesKitty:
		return artKitty
	}
	switch {
	case m.termKitty && m.kittyNamed:
		return artKitty
	case m.termSixel:
		return artSixel
	}
	return artBlocks
}

// kittyPlaceholderTerm reports whether an XTVERSION name is a terminal known
// to render kitty Unicode placeholders. Answering the graphics query is not
// enough: other terminals implement parts of the protocol without them.
func kittyPlaceholderTerm(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "kitty") || strings.HasPrefix(n, "ghostty")
}

// kittyPlaceholderEnv is kittyPlaceholderTerm for terminals identified by
// their environment instead of an XTVERSION reply.
func kittyPlaceholderEnv() bool {
	switch os.Getenv("TERM") {
	case "xterm-kitty", "xterm-ghostty":
		return true
	}
	return os.Getenv("TERM_PROGRAM") == "ghostty"
}

// cellPx is the terminal's cell size in pixels (CSI 16 t), with the common
// 10x20 default until it answers.
func (m Model) cellPx() (int, int) {
	if m.cellW > 0 && m.cellH > 0 {
		return m.cellW, m.cellH
	}
	return 10, 20
}

// insidePsmux reports whether cliamp runs in a psmux pane. psmux (tmux for
// Windows) re-renders panes through ConPTY and passes no image sequences
// on, but its answer to the attributes query can still claim Sixel, so auto
// mode must not trust that answer there.
func insidePsmux() bool { return os.Getenv("PSMUX_SESSION") != "" }

// termImageQueries asks the terminal for its cell size in pixels and its
// device attributes (Sixel is attribute 4). In auto and kitty modes it
// first probes kitty graphics and asks for the terminal's name (XTVERSION);
// the replies arrive in order, so they are in before the DA1 answer. One
// Raw keeps the order on the wire.
func (m Model) termImageQueries() tea.Cmd {
	q := "\x1b[16t"
	if m.imgMode == imagesAuto || m.imgMode == imagesKitty {
		q = termimg.KittyQuery() + ansi.RequestNameVersion + q
	}
	return tea.Raw(q + ansi.RequestPrimaryDeviceAttributes)
}

// handleTermImageEvent records terminal replies. It reports whether msg was
// one of them.
func (m *Model) handleTermImageEvent(msg tea.Msg) bool {
	switch ev := msg.(type) {
	case uv.PrimaryDeviceAttributesEvent:
		for _, a := range ev {
			if a == 4 && !insidePsmux() {
				m.termSixel = true
			}
		}
		applog.Debug("images: terminal attributes %v, sixel=%v, kitty=%v, placeholders=%v, mode=%d, using %d",
			[]int(ev), m.termSixel, m.termKitty, m.kittyNamed, m.imgMode, m.artKindNow())
		return true
	case uv.KittyGraphicsEvent:
		if ev.Options.ID == termimg.KittyQueryID && string(ev.Payload) == "OK" {
			m.termKitty = true
		}
		return true
	case tea.TerminalVersionMsg:
		m.kittyNamed = m.kittyNamed || kittyPlaceholderTerm(ev.Name)
		applog.Debug("images: terminal %q", ev.Name)
		return true
	case uv.CellSizeEvent:
		if ev.Width > 0 && ev.Height > 0 && (ev.Width != m.cellW || ev.Height != m.cellH) {
			m.cellW, m.cellH = ev.Width, ev.Height
			m.imgLayer.Invalidate()
			applog.Debug("images: cell size %dx%d px", ev.Width, ev.Height)
		}
		return true
	}
	return false
}

// — requests —

type artPollMsg struct{}

type artFetchedMsg struct {
	url string
	img image.Image
	err error
}

type artEncodedMsg struct {
	key     artKey
	sixel   []byte
	blocks  []string
	kittyID uint32
	kitty   []byte // transmit sequence
}

func artPollCmd() tea.Cmd {
	return tea.Tick(artPollEvery, func(time.Time) tea.Msg { return artPollMsg{} })
}

// artSlot is one art box in the current frame: the cover URL and the box in
// frame content coordinates.
type artSlot struct {
	url  string
	rect immRect
}

// placeholderArtScheme marks a generated cover: items without artwork get
// a "cliamp-art:<name>" URL, and loadArt draws a placeholder tile for it
// instead of fetching, so placeholders go through the same encode path
// (Sixel, kitty or half-blocks) as real covers.
const placeholderArtScheme = "cliamp-art:"

// artURLFor returns the cover URL for an art box: the real cover when the
// item has one, otherwise a generated placeholder seeded by the item name.
func artURLFor(u, name string) string {
	if u != "" || name == "" {
		return u
	}
	return placeholderArtScheme + name
}

// immNowPlayingArt is the Now Playing box's art URL and the name its
// placeholder is seeded with ("cliamp" when nothing is loaded).
func (m Model) immNowPlayingArt() (string, string) {
	track, _ := m.currentPlaybackTrack()
	name := track.Title
	if name == "" {
		name = trackViewName(track)
	}
	return artURLFor(track.AlbumArtURL, firstNonEmpty(track.Album, name, "cliamp")), name
}

// immArtSlots lists the art boxes the current frame draws.
func (m Model) immArtSlots() []artSlot {
	if !m.immersiveShown() || m.fullVis {
		return nil
	}
	g := m.immGeom()
	var slots []artSlot
	if u, _ := m.immNowPlayingArt(); g.npArt.W > 0 && g.npArt.H > 0 && !m.immSuggestCovers(g.npArt) {
		slots = append(slots, artSlot{url: u, rect: g.npArt})
	}
	if m.immersive.mode == immCanvasList || m.immersive.view == immViewSettings {
		return slots
	}
	items := m.canvasItems()
	for _, it := range m.immCanvasItemsGeom(g.canvasIW, g.canvasIH) {
		if it.idx < len(items) && it.art.W > 0 && it.art.H > 0 && !m.immSuggestCovers(it.art) {
			if u := artURLFor(items[it.idx].art, items[it.idx].title); u != "" {
				slots = append(slots, artSlot{url: u, rect: it.art})
			}
		}
	}
	return slots
}

// immArtRequests starts fetches and encodes for art boxes on screen that
// have no cover yet. It is driven by artPollMsg while immersive is active.
func (m *Model) immArtRequests() tea.Cmd {
	kind := m.artKindNow()
	if kind == artNone {
		return nil
	}
	cw, ch := m.cellPx()
	a := m.art
	var del []byte // kitty deletes, written once the lock is released
	defer func() { m.kittyWrite(del) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	var cmds []tea.Cmd
	slots := m.immArtSlots()
	for _, s := range slots {
		im := a.imgs[s.url]
		switch {
		case im == nil:
			if a.inflight >= artMaxInflight {
				continue
			}
			if len(a.imgs) >= artMaxImages {
				del = append(del, m.dropKittyLocked(nil)...)
				a.imgs, a.encs = map[string]*artImage{}, map[artKey]*artEncoding{}
			}
			a.imgs[s.url] = &artImage{loading: true}
			a.inflight++
			cmds = append(cmds, fetchArtCmd(s.url))
		case im.img != nil:
			key := artKey{url: s.url, w: s.rect.W, h: s.rect.H, cellW: cw, cellH: ch, kind: kind}
			if a.encs[key] == nil {
				if len(a.encs) >= artMaxEncodings {
					del = append(del, m.dropKittyLocked(nil)...)
					a.encs = map[artKey]*artEncoding{}
				}
				e := &artEncoding{loading: true}
				if kind == artKitty {
					if a.kittyCountLocked() >= artMaxKitty {
						del = append(del, m.dropKittyLocked(func(k artKey) bool { return slotsHave(slots, k) })...)
					}
					e.kittyID = termimg.KittyID(a.kittySeq % (termimg.KittyIDs - 1))
					a.kittySeq++
				}
				a.encs[key] = e
				cmds = append(cmds, encodeArtCmd(key, im.img, cw, ch, e.kittyID, m.imgTmux))
			}
		}
	}
	return tea.Batch(cmds...)
}

// handleArtMsg applies fetch/encode results and the poll tick.
func (m *Model) handleArtMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case artPollMsg:
		if !m.immersive.active || m.art == nil {
			m.artPolling = false
			return nil, true
		}
		return tea.Batch(m.immArtRequests(), artPollCmd()), true
	case artFetchedMsg:
		if m.art == nil {
			return nil, true
		}
		m.art.mu.Lock()
		m.art.inflight = max(0, m.art.inflight-1)
		if im := m.art.imgs[msg.url]; im != nil {
			im.loading = false
			im.img = msg.img
			im.failed = msg.err != nil
		}
		if msg.err != nil {
			applog.Debug("images: cover %s: %v", msg.url, msg.err)
		}
		m.art.mu.Unlock()
		return m.immArtRequests(), true
	case artEncodedMsg:
		if m.art == nil {
			return nil, true
		}
		var transmit []byte
		m.art.mu.Lock()
		if e := m.art.encs[msg.key]; e != nil && e.kittyID == msg.kittyID {
			e.loading = false
			e.sixel, e.blocks = msg.sixel, msg.blocks
			if len(msg.kitty) > 0 && m.imgOut != nil {
				transmit = msg.kitty
				e.kitty = termimg.KittyPlaceholders(e.kittyID, msg.key.w, msg.key.h)
			}
		}
		m.art.mu.Unlock()
		// Sent before this Update returns, so it lands ahead of the frame
		// that first shows the placeholder cells.
		m.kittyWrite(transmit)
		return nil, true
	}
	return nil, false
}

// startArtPolling begins the art loop once per immersive session.
func (m *Model) startArtPolling() tea.Cmd {
	if m.artPolling || m.art == nil || m.imgMode == imagesOff {
		return nil
	}
	m.artPolling = true
	return tea.Batch(artPollCmd(), m.termImageQueries())
}

// — kitty image lifetime —

// kittyWrite sends kitty graphics sequences to the terminal. They are out
// of band (no cursor movement, no cells), so they may land between frames.
func (m Model) kittyWrite(seq []byte) {
	if m.imgOut == nil || len(seq) == 0 {
		return
	}
	if _, err := m.imgOut.Write(seq); err != nil {
		applog.Debug("images: kitty write: %v", err)
	}
}

// kittyCountLocked counts the kitty encodings. Caller holds a.mu.
func (a *artStore) kittyCountLocked() int {
	n := 0
	for k := range a.encs {
		if k.kind == artKitty {
			n++
		}
	}
	return n
}

// dropKittyLocked forgets every kitty encoding keep rejects (all of them
// when keep is nil) and returns the sequences that delete their images from
// the terminal. An encode still running for a dropped key is discarded when
// it lands. Caller holds m.art.mu.
func (m Model) dropKittyLocked(keep func(artKey) bool) []byte {
	var del []byte
	maps.DeleteFunc(m.art.encs, func(k artKey, e *artEncoding) bool {
		if k.kind != artKitty || (keep != nil && keep(k)) {
			return false
		}
		if e.kitty != nil {
			del = append(del, termimg.KittyDelete(e.kittyID, m.imgTmux)...)
		}
		return true
	})
	return del
}

// releaseImages deletes every kitty image cliamp put in the terminal:
// leaving immersive or quitting. Deleting the visualizer image when it was
// never sent is harmless (q=2 silences the error).
func (m Model) releaseImages() {
	if m.art == nil || m.imgOut == nil {
		return
	}
	m.art.mu.Lock()
	del := m.dropKittyLocked(nil)
	m.art.mu.Unlock()
	if m.artKindNow() == artKitty {
		del = append(del, termimg.KittyDelete(kittyVisID, m.imgTmux)...)
		if m.pixVis != nil {
			m.pixVis.resend.Store(true)
		}
	}
	m.kittyWrite(del)
}

// slotsHave reports whether k is the encoding of one of the frame's boxes.
func slotsHave(slots []artSlot, k artKey) bool {
	for _, s := range slots {
		if s.url == k.url && s.rect.W == k.w && s.rect.H == k.h {
			return true
		}
	}
	return false
}

func fetchArtCmd(u string) tea.Cmd {
	return func() tea.Msg {
		img, err := loadArt(u)
		return artFetchedMsg{url: u, img: img, err: err}
	}
}

// placeholderArtSide is the pixel size placeholder tiles are drawn at; the
// encoder scales them to the box like any cover.
const placeholderArtSide = 256

// loadArt fetches and decodes one cover from an http(s) or file:// URL, or
// draws a generated placeholder for a cliamp-art: URL.
func loadArt(u string) (image.Image, error) {
	var r io.ReadCloser
	switch {
	case strings.HasPrefix(u, placeholderArtScheme):
		return termimg.PlaceholderArt(strings.TrimPrefix(u, placeholderArtScheme), placeholderArtSide), nil
	case strings.HasPrefix(u, "file://"):
		p, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("cover url: %w", err)
		}
		path := p.Path
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:] // file:///C:/... on Windows
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open cover: %w", err)
		}
		r = f
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		ctx, cancel := context.WithTimeout(context.Background(), artFetchTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("cover request: %w", err)
		}
		req.Header.Set("User-Agent", httpclient.UserAgent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch cover: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("fetch cover: %s", resp.Status)
		}
		r = resp.Body
	default:
		return nil, errors.New("cover: unsupported url")
	}
	defer r.Close()
	img, _, err := image.Decode(io.LimitReader(r, artMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("decode cover: %w", err)
	}
	// Keep the cache small: podcast art is often 3000px square.
	if b := img.Bounds(); max(b.Dx(), b.Dy()) > artMaxSide {
		w, h := b.Dx(), b.Dy()
		if w >= h {
			h, w = max(1, h*artMaxSide/w), artMaxSide
		} else {
			w, h = max(1, w*artMaxSide/h), artMaxSide
		}
		return termimg.Fill(img, w, h), nil
	}
	return img, nil
}

func encodeArtCmd(key artKey, img image.Image, cellW, cellH int, kittyID uint32, tmux bool) tea.Cmd {
	return func() tea.Msg {
		switch key.kind {
		case artSixel:
			px := termimg.Fill(img, key.w*cellW, key.h*cellH)
			return artEncodedMsg{key: key, sixel: termimg.EncodeSixel(px)}
		case artKitty:
			px := termimg.Fill(img, key.w*cellW, key.h*cellH)
			return artEncodedMsg{key: key, kittyID: kittyID, kitty: termimg.KittyTransmit(kittyID, px, key.w, key.h, tmux)}
		default:
			px := termimg.Fill(img, key.w, key.h*2)
			return artEncodedMsg{key: key, blocks: termimg.HalfBlocks(px)}
		}
	}
}

// — drawing —

// immArtCells returns what the text frame shows in an art box: kitty
// placeholder cells, blanks under a ready Sixel cover, the half-block
// cover, or ok=false to keep the placeholder.
func (m Model) immArtCells(u string, w, h int) ([]string, bool) {
	kind := m.artKindNow()
	if kind == artNone || u == "" {
		return nil, false
	}
	m.art.mu.Lock()
	cw, ch := m.cellPx()
	e := m.art.encs[artKey{url: u, w: w, h: h, cellW: cw, cellH: ch, kind: kind}]
	m.art.mu.Unlock()
	switch {
	case e == nil || e.loading:
		return nil, false
	case kind == artSixel && len(e.sixel) > 0:
		lines := make([]string, h)
		for i := range lines {
			lines[i] = strings.Repeat(" ", w)
		}
		return lines, true
	case kind == artBlocks && len(e.blocks) > 0:
		return e.blocks, true
	case kind == artKitty && e.kitty != nil:
		return e.kitty, true
	}
	return nil, false
}

// clearImages removes every image (screens without art, quitting, the
// too-small notice) and resets the row memo.
func (m Model) clearImages() {
	m.imgLayer.Set(nil)
	m.imgLayer.ArmLive(0, 0, 0, 0)
	m.touchChangedRows("")
}

// frameMemo keeps the last view's lines so View can tell the image layer
// which screen rows changed (see termimg.Layer.Touch).
type frameMemo struct{ lines []string }

// touchChangedRows marks rows whose text differs from the previous view;
// "" (a non-immersive view) resets the memo so the next immersive view
// touches every row.
func (m Model) touchChangedRows(rendered string) {
	if m.frameMemo == nil || m.imgLayer == nil {
		return
	}
	if rendered == "" {
		m.frameMemo.lines = nil
		return
	}
	lines := strings.Split(rendered, "\n")
	var rows []int
	for i, l := range lines {
		if i >= len(m.frameMemo.lines) || m.frameMemo.lines[i] != l {
			rows = append(rows, i)
		}
	}
	m.frameMemo.lines = lines
	m.imgLayer.Touch(rows...)
}

// immArtPlacements lists the Sixel covers to draw over this frame's blank
// art boxes, in screen cells.
func (m Model) immArtPlacements() []termimg.Placement {
	if m.artKindNow() != artSixel {
		return nil
	}
	slots := m.immArtSlots()
	if len(slots) == 0 {
		return nil
	}
	ox, oy := m.layout.paddingH, m.layout.paddingV
	cw, ch := m.cellPx()
	ps := make([]termimg.Placement, 0, len(slots))
	m.art.mu.Lock()
	defer m.art.mu.Unlock()
	for _, s := range slots {
		e := m.art.encs[artKey{url: s.url, w: s.rect.W, h: s.rect.H, cellW: cw, cellH: ch, kind: artSixel}]
		if e == nil || len(e.sixel) == 0 {
			continue
		}
		ps = append(ps, termimg.Placement{
			Key: s.url, X: ox + s.rect.X, Y: oy + s.rect.Y, W: s.rect.W, H: s.rect.H, Data: e.sixel,
		})
	}
	return ps
}
