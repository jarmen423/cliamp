package model

import (
	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/internal/playback"
	"github.com/bjarneo/cliamp/ui"
)

// Update handles messages: key presses, ticks, and window resizes. After each
// message it lays out the frame for View, drops a gapless preload that no
// longer matches the next track, and it tells the media controls and plugins
// when the playback state changed.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(spinnerTickMsg); ok {
		m.spinnerTicking = m.spinnerVisible()
		if !m.spinnerTicking {
			return m, nil
		}
		return m, spinnerTickCmd()
	}
	spinning := m.spinnerVisible()
	next, cmd := m.update(msg)
	if nm, ok := next.(Model); ok {
		nm.recomputeLayout()
		nm.dropStalePreload()
		nm.notifyPlaybackChange()
		// A load that starts now gets its own redraws at once. The main tick
		// can still wait up to ui.TickIdle before it runs at the spinner rate.
		if !spinning && !nm.spinnerTicking && nm.spinnerVisible() {
			nm.spinnerTicking = true
			cmd = tea.Batch(cmd, spinnerTickCmd())
		}
		next = nm
	}
	return next, cmd
}

// update handles one message. Update then lays out the frame, drops a stale
// preload, tells the media controls and plugins about a playback change and
// starts the spinner tick.
func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.handleTermImageEvent(msg) {
		return m, nil
	}
	if cmd, ok := m.handleArtMsg(msg); ok {
		return m, cmd
	}
	wasScreen := m.activeScreen()
	wasVisualizerVisible := m.visualizerVisible()
	wasMode := ui.VisNone
	if m.vis != nil {
		wasMode = m.vis.Mode
	}
	wasPlaying := false
	wasPaused := false
	if m.player != nil {
		wasPlaying = m.player.IsPlaying()
		wasPaused = m.player.IsPaused()
	}
	defer func() {
		m.maybeRequestVisualizerRefresh(msg, wasScreen, wasVisualizerVisible, wasMode, wasPlaying, wasPaused)
		m.emitPluginEvents()
		m.publishIPCRuntimeState()
		m.publishPluginState()
	}()

	switch msg := msg.(type) {
	case tea.PasteMsg:
		cmd := m.handlePaste(msg.Content)
		return m, cmd

	case tea.KeyPressMsg:
		cmd := m.handleKey(msg)
		if m.quitting {
			return m, tea.Quit
		}
		m.applyHeightMode()
		m.adjustScroll()
		return m, cmd

	case tea.MouseClickMsg:
		return m, m.handleMouseClick(msg)

	case tea.MouseReleaseMsg:
		return m, m.handleMouseRelease()

	case tea.MouseMotionMsg:
		return m, m.handleMouseMotion(msg)

	case tea.MouseWheelMsg:
		return m, m.handleMouseWheel(msg)

	case trackRadioMsg:
		return m, m.handleTrackRadio(msg)

	case collectionRadioMsg:
		return m, m.handleCollectionRadio(msg)

	case menuArtistMsg:
		if msg.gen != m.requests.trackMenu {
			return m, nil
		}
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Artist lookup failed: %s", msg.err)
			return m, nil
		}
		if m.immersiveOwns(msg.providerName) {
			return m, m.openImmersiveItem(immItem{kind: immKindArtist, id: msg.artist.ID, title: msg.artist.Name, sub: "Artist", art: msg.artist.ImageURL})
		}
		if m.immersiveShown() {
			m.status.Showf(statusTTLDefault, "That %s is on %s: leave immersive (I) to open it", "artist", msg.providerName)
			return m, nil
		}
		return m, m.openArtistScreen(msg.providerName, msg.artist)

	case menuAlbumMsg:
		if msg.gen != m.requests.trackMenu {
			return m, nil
		}
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Album lookup failed: %s", msg.err)
			return m, nil
		}
		if m.immersiveOwns(msg.providerName) {
			a := msg.album
			return m, m.openImmersiveItem(immItem{kind: immKindAlbum, id: a.ID, title: a.Name, sub: firstNonEmpty(a.Artist, "Album"), art: a.ImageURL})
		}
		if m.immersiveShown() {
			m.status.Showf(statusTTLDefault, "That %s is on %s: leave immersive (I) to open it", "album", msg.providerName)
			return m, nil
		}
		return m, m.openResolvedAlbum(msg)

	case autoPlayMsg:
		if m.playlist.Len() > 0 && !m.player.IsPlaying() {
			cmd := m.playCurrentTrack()
			return m, cmd
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recomputeLayout()
		m.normalizeMainFocus()
		m.clampActiveScrollState()
		if m.art != nil && m.immersive.active {
			// A font zoom arrives as a size change; re-ask the cell size so
			// covers and the pixel visualizer are encoded for the new one.
			return m, tea.Raw("\x1b[16t")
		}
		return m, nil

	case seekTickMsg:
		cmd := m.handleSeekTick(msg)
		return m, cmd

	case ytdlUnpauseReconnectMsg:
		m.handleYTDLUnpauseReconnect(msg)
		return m, nil

	case tickMsg:
		cmd := m.handleTick(msg)
		return m, cmd

	case openDefaultProviderBrowserMsg:
		if !m.openDefaultProviderOnce {
			return m, nil
		}
		m.openDefaultProviderOnce = false
		cmd := m.openDefaultProviderBrowser()
		return m, cmd

	case openImmersiveMsg:
		if !m.openImmersiveOnce {
			return m, nil
		}
		m.openImmersiveOnce = false
		// Start regardless of the current size: the first WindowSizeMsg may
		// not have arrived yet, and a too-small terminal only hides the frame
		// until it grows (immersiveShown).
		return m, m.startImmersive()

	case radioListsRefreshMsg:
		if msg.gen != m.requests.provider || m.activeProviderKey() != providerKeyRadio {
			return m, nil
		}
		cmd := m.refreshRadioLists()
		return m, cmd

	case playlistsLoadedMsg:
		cmd := m.handlePlaylistsLoaded(msg)
		return m, cmd

	case radioListenersLoadedMsg:
		m.handleRadioListenersLoaded(msg)
		return m, nil

	case smartRecommendsMsg:
		m.handleSmartRecommends(msg)
		return m, nil

	case tracksLoadedMsg:
		cmd := m.handleTracksLoaded(msg)
		return m, cmd

	case navArtistsLoadedMsg:
		m.handleNavArtistsLoaded(msg)
		return m, nil

	case navAlbumsLoadedMsg:
		m.handleNavAlbumsLoaded(msg)
		return m, nil

	case navGenresLoadedMsg:
		m.handleNavGenresLoaded(msg)
		return m, nil

	case navTracksLoadedMsg:
		m.handleNavTracksLoaded(msg)
		return m, nil

	case catalogBatchMsg:
		m.handleCatalogBatch(msg)
		return m, nil

	case catalogSearchMsg:
		m.handleCatalogSearch(msg)
		return m, nil

	case ytdlBatchMsg:
		cmd := m.handleYTDLBatch(msg)
		return m, cmd

	case feedTrackResolvedMsg:
		cmd := m.handleFeedTrackResolved(msg)
		return m, cmd

	case subsEpisodesMsg:
		cmd := m.handleSubsEpisodes(msg)
		return m, cmd

	case subsLatestAllMsg:
		cmd := m.handleSubsLatestAll(msg)
		return m, cmd

	case feedsLoadedMsg:
		cmd := m.handleFeedsLoaded(msg)
		return m, cmd

	case netSearchResultsMsg:
		m.handleNetSearchResults(msg)
		return m, nil

	case lyricsLoadedMsg:
		if msg.gen != m.requests.lyrics || !m.lyrics.visible || msg.query != m.lyrics.query {
			return m, nil
		}
		m.lyrics.loading = false
		m.lyrics.err = msg.err
		m.lyrics.scroll = 0
		if msg.err == nil {
			m.lyrics.lines = msg.lines
		}
		return m, nil

	case fbTracksResolvedMsg:
		cmd := m.handleFBTracksResolved(msg)
		return m, cmd

	case streamPlayedMsg:
		cmd := m.handleStreamPlayed(msg)
		return m, cmd

	case streamPreloadedMsg:
		if msg.gen != m.requests.preload {
			return m, nil
		}
		m.preloading = false
		if msg.err != nil {
			// Playback falls back to a non-gapless start for this track.
			// Retrying on the next tick would rebuild the failing pipeline.
			m.preloadFailed = msg.path
		}
		return m, nil

	case trackSavedMsg:
		m.handleTrackSaved(msg)
		return m, nil

	case searchOverlayResultsMsg:
		m.handleSearchOverlayResults(msg)
		return m, nil

	case searchOverlayAlbumTracksMsg:
		cmd := m.handleSearchOverlayAlbumTracks(msg)
		return m, cmd

	case searchOverlayPlaylistsMsg:
		m.handleSearchOverlayPlaylists(msg)
		return m, nil

	case searchOverlayAddedMsg:
		m.handleSearchOverlayAdded(msg)
		return m, nil

	case searchOverlayCreatedMsg:
		m.handleSearchOverlayCreated(msg)
		return m, nil

	case spotSearchResultsMsg:
		if !m.isCurrentSpotRequest(msg.gen, msg.providerName) || m.spotSearch.query != msg.query {
			return m, nil
		}
		m.cancelSpotRequest()
		m.spotSearch.loading = false
		m.spotSearch.cursor = 0
		m.spotSearch.scroll = 0
		if msg.err != nil {
			m.setSpotSearchError(msg.err.Error())
			return m, nil
		}
		m.spotSearch.results = msg.tracks
		m.spotSearch.cursor = 0
		m.spotSearch.screen = spotSearchResults
		m.applyHeightMode()
		m.clampActiveScrollState()
		return m, nil

	case spotAlbumTracksMsg:
		if msg.gen != m.requests.spotAlbum {
			return m, nil
		}
		m.cancelSpotRequest()
		m.spotSearch.albumLoading = false
		if msg.err != nil {
			m.setSpotSearchError(msg.err.Error())
			return m, nil
		}
		if len(msg.tracks) == 0 {
			m.setSpotSearchError("That album has no tracks available here.")
			return m, nil
		}
		album := msg.album
		tracks := msg.tracks
		m.closeSpotSearch()
		switch msg.action {
		case spotAlbumAppend:
			cmd := m.appendAlbum(album, tracks)
			return m, cmd
		case spotAlbumQueueNext:
			cmd := m.queueAlbumNext(album, tracks)
			return m, cmd
		default:
			cmd := m.playAlbumImmediate(album, tracks)
			return m, cmd
		}

	case spotPlaylistsMsg:
		if !m.isCurrentSpotListRequest(msg.gen, msg.providerName) {
			return m, nil
		}
		m.spotSearch.loading = false
		m.spotSearch.cursor = 0
		m.spotSearch.scroll = 0
		if msg.err != nil {
			m.setSpotSearchError(msg.err.Error())
			return m, nil
		}
		m.spotSearch.playlists = msg.playlists
		m.spotSearch.cursor = 0
		m.spotSearch.screen = spotSearchPlaylist
		m.applyHeightMode()
		m.clampActiveScrollState()
		return m, nil

	case spotAddedMsg:
		if !m.isCurrentSpotMutation(msg.gen, msg.providerName) {
			return m, nil
		}
		m.cancelSpotRequest()
		m.spotSearch.loading = false
		if msg.err != nil {
			m.setSpotSearchError("Add failed: " + msg.err.Error())
			return m, nil
		}
		m.status.Showf(statusTTLDefault, "Added to %q", msg.name)
		m.closeSpotSearch()
		return m, nil

	case spotCreatedMsg:
		if !m.isCurrentSpotMutation(msg.gen, msg.providerName) {
			return m, nil
		}
		m.cancelSpotRequest()
		m.spotSearch.loading = false
		if msg.err != nil {
			m.setSpotSearchError("Create failed: " + msg.err.Error())
			return m, nil
		}
		m.status.Showf(statusTTLDefault, "Created %q & added track", msg.name)
		m.closeSpotSearch()
		return m, nil

	case spotSearchAllMsg:
		if !m.isCurrentSpotRequest(msg.gen, msg.providerName) || m.spotSearch.query != msg.query {
			return m, nil
		}
		m.cancelSpotRequest()
		m.spotSearch.loading = false
		if msg.err != nil {
			m.spotSearch.err = msg.err.Error()
			return m, nil
		}
		m.showSpotResultsAll(msg.results, spotTabTracks, "No results found")
		return m, nil

	case spotCategoriesMsg:
		m.handleSpotCategoriesMsg(msg)
		return m, nil

	case spotCategoryMsg:
		m.handleSpotCategoryMsg(msg)
		return m, nil

	case spotDrillLoadedMsg:
		if !m.isCurrentSpotRequest(msg.gen, msg.providerName) || len(m.spotSearch.drill) == 0 {
			return m, nil
		}
		lvl := &m.spotSearch.drill[len(m.spotSearch.drill)-1]
		if lvl.crumb != msg.crumb {
			return m, nil
		}
		lvl.loading = false
		if msg.err != nil {
			m.spotSearch.drill = m.spotSearch.drill[:len(m.spotSearch.drill)-1]
			m.spotSearch.err = "Load failed: " + msg.err.Error()
			return m, nil
		}
		lvl.albums = msg.albums
		lvl.tracks = msg.tracks
		lvl.cursor = 0
		lvl.scroll = 0
		if m.spotDrillCount(*lvl) == 0 {
			m.spotSearch.err = "No tracks found"
		}
		m.applyHeightMode()
		m.clampActiveScrollState()
		return m, nil

	case artistDetailMsg:
		if !m.isCurrentArtistRequest(msg.gen, msg.providerName) || msg.artistID != m.artist.info.ID {
			return m, nil
		}
		m.cancelArtistRequest()
		m.artist.loading = false
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Artist load failed: %s", msg.err)
			m.closeArtistScreen()
			return m, nil
		}
		m.artist.detail = msg.detail
		m.artist.cursor = 0
		m.artist.scroll = 0
		m.applyHeightMode()
		m.clampActiveScrollState()
		return m, nil

	case artistAlbumTracksMsg:
		if !m.isCurrentArtistRequest(msg.gen, msg.providerName) || msg.artistID != m.artist.info.ID || len(m.artist.drill) == 0 {
			return m, nil
		}
		lvl := &m.artist.drill[len(m.artist.drill)-1]
		if lvl.crumb != msg.crumb {
			return m, nil
		}
		lvl.loading = false
		if msg.err != nil {
			m.artist.drill = m.artist.drill[:len(m.artist.drill)-1]
			m.status.Showf(statusTTLDefault, "Album load failed: %s", msg.err)
			return m, nil
		}
		lvl.tracks = msg.tracks
		lvl.cursor = 0
		lvl.scroll = 0
		if len(lvl.tracks) == 0 {
			m.status.Show("No tracks found", statusTTLDefault)
		}
		m.applyHeightMode()
		m.clampActiveScrollState()
		return m, nil

	case homeListsMsg:
		if !m.isCurrentHomeSectionRequest(msg.gen, msg.providerName, &m.requests.homeLists) {
			return m, nil
		}
		m.handleHomeLists(msg)
		return m, nil

	case homeAlbumsMsg:
		if !m.isCurrentHomeSectionRequest(msg.gen, msg.providerName, &m.requests.homeAlbums) {
			return m, nil
		}
		m.handleHomeAlbums(msg)
		return m, nil

	case homeArtistsMsg:
		if !m.isCurrentHomeSectionRequest(msg.gen, msg.providerName, &m.requests.homeArtists) {
			return m, nil
		}
		m.handleHomeArtists(msg)
		return m, nil

	case homeContentMsg:
		if !m.isCurrentHomeContentRequest(msg.gen, msg.providerName, msg.kind, msg.id) {
			return m, nil
		}
		return m, m.handleHomeContent(msg)

	case homePageMsg:
		if !m.isCurrentHomeContentRequest(msg.gen, msg.providerName, homeContentPlaylist, msg.playlistID) {
			return m, nil
		}
		return m, m.handleHomePage(msg)

	case homeCreatedMsg:
		if !m.isCurrentHomeSectionRequest(msg.gen, msg.providerName, &m.requests.homeCreate) {
			return m, nil
		}
		m.home.creating = false
		if msg.err != nil {
			m.home.inputErr = "Create failed: " + msg.err.Error()
			return m, nil
		}
		m.status.Showf(statusTTLDefault, "Created %q", msg.name)
		m.home.screen = homeScreenLibrary
		m.home.newName = ""
		m.home.inputErr = ""
		m.home.fixupID = msg.playlistID
		m.home.loadingLists = true
		return m, fetchHomeListsCmd(m.home.prov, msg.providerName, nextRequest(&m.requests.homeLists))

	case immersiveListsMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveLists) {
			return m, nil
		}
		if msg.partial {
			// The first rows show while the full list loads. Once the full
			// list is in, or when they fail, they change nothing; the full
			// answer reports any error and runs the page restore.
			if m.immersive.loadingLists && msg.err == nil {
				m.immersive.loadingLists = false
				m.immersive.lists = m.filterImmersivePlaylists(msg.lists)
			}
			return m, nil
		}
		m.immersive.loadingLists = false
		m.noteImmersiveLoadErr("Playlists", msg.err)
		if msg.err == nil {
			m.immersive.lists = m.filterImmersivePlaylists(msg.lists)
			m.applyImmersiveRestorePos()
		}
		return m, m.immersiveRestoreListsLoaded(msg.err)

	case immersiveAlbumsMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveAlbums) {
			return m, nil
		}
		m.immersive.loadingAlbums = false
		m.noteImmersiveLoadErr("Albums", msg.err)
		if msg.err == nil {
			m.immersive.albums = msg.albums
			m.applyImmersiveRestorePos()
		}
		return m, nil

	case immersiveArtistsMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveArtists) {
			return m, nil
		}
		m.immersive.loadingArtists = false
		m.noteImmersiveLoadErr("Artists", msg.err)
		if msg.err == nil {
			m.immersive.artists = msg.artists
			m.applyImmersiveRestorePos()
		}
		return m, nil

	case immersiveContentMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveContent) {
			return m, nil
		}
		m.immersive.tracksLoading = false
		m.immersive.ctxID, m.immersive.ctxName, m.immersive.ctxSub = msg.id, msg.name, msg.sub
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Load failed: %s", msg.err)
			m.immersiveRestoreFailed()
			return m, nil
		}
		m.immersive.tracks = msg.tracks
		m.applyImmersiveRestorePos()
		return m, nil

	case immersiveArtistMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveArtist) {
			return m, nil
		}
		m.immersive.tracksLoading = false
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Artist load failed: %s", msg.err)
			m.immersiveRestoreFailed()
			return m, nil
		}
		m.immersive.tracks = msg.detail.Popular
		m.immersive.ctxSub = msg.detail.Info.Name
		m.applyImmersiveRestorePos()
		return m, nil

	case immSuggestTickMsg, immSuggestMsg:
		cmd, _ := m.handleImmersiveSuggestMsg(msg)
		return m, cmd

	case immersiveSearchMsg:
		if !m.isCurrentImmersiveRequest(msg.gen, msg.providerName, m.requests.immersiveSearch) {
			return m, nil
		}
		m.immersive.searchLoading = false
		m.immersive.tracksLoading = false
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Search failed: %s", msg.err)
			m.immersiveRestoreFailed()
			return m, nil
		}
		m.immersive.tracks = msg.tracks
		m.applyImmersiveRestorePos()
		return m, nil

	case trackLikeToggledMsg:
		if msg.gen != m.requests.like {
			return m, nil
		}
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Like failed: %s", msg.err)
			return m, nil
		}
		if msg.liked {
			m.status.Success("Added to liked tracks", statusTTLDefault)
		} else {
			m.status.Success("Removed from liked tracks", statusTTLDefault)
		}
		return m, nil

	case playlistUnfollowedMsg:
		if msg.gen != m.requests.provMutation || !m.isActiveProvider(msg.providerName) {
			return m, nil
		}
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Delete failed: %s", msg.err)
			return m, nil
		}
		if msg.owned {
			m.status.Showf(statusTTLDefault, "Deleted %q", msg.name)
		} else {
			m.status.Showf(statusTTLDefault, "Unfollowed %q", msg.name)
		}
		if msg.playlistID == m.activeProviderPlaylistID {
			m.resetProviderQueueMirror()
		}
		// Refresh the list off the Update goroutine; the resulting
		// playlistsLoadedMsg clamps the cursor to the shrunken list.
		m.provListFixup = provListFixupState{clampCursor: true}
		return m, m.fetchProviderPlaylists()

	case playlistRenamedMsg:
		if msg.gen != m.requests.provMutation || !m.isActiveProvider(msg.providerName) {
			return m, nil
		}
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Rename failed: %s", msg.err)
			return m, nil
		}
		m.status.Showf(statusTTLDefault, "Renamed to %q", msg.newName)
		// Refresh the list off the Update goroutine; the resulting
		// playlistsLoadedMsg reselects the renamed row.
		m.provListFixup = provListFixupState{selectID: msg.playlistID}
		return m, m.fetchProviderPlaylists()

	case remoteTrackRemovedMsg:
		// Removes are matched against the queue mirror (playlist + position +
		// track) rather than only the mutation generation: a second remove
		// issued while one is in flight bumps the generation and would
		// otherwise discard the first completion after the remote removal
		// already succeeded.
		if !m.isActiveProvider(msg.providerName) {
			return m, nil
		}
		if msg.err != nil {
			if msg.gen == m.requests.provMutation {
				m.status.Showf(statusTTLDefault, "Remove failed: %s", msg.err)
			}
			return m, nil
		}
		if msg.gen == m.requests.provMutation {
			m.status.Successf(statusTTLDefault, "Removed from playlist: %s", msg.trackName)
		}
		wasActive := msg.position == m.playlist.Index()
		if tracks := m.playlist.Tracks(); msg.playlistID == m.activeProviderPlaylistID &&
			msg.position >= 0 && msg.position < len(tracks) && tracks[msg.position].Path == msg.trackPath {
			m.playlist.Remove(msg.position)
			if m.providerQueueLen > msg.position {
				m.providerQueueLen--
			}
			if tracks := m.playlist.Tracks(); m.providerQueueLen > 0 && m.providerQueueLen <= len(tracks) {
				m.providerQueueLastPath = tracks[m.providerQueueLen-1].Path
			} else {
				m.providerQueueLastPath = ""
			}
			if newLen := m.playlist.Len(); newLen == 0 {
				m.plCursor = 0
			} else if m.plCursor >= newLen {
				m.plCursor = newLen - 1
			}
			if wasActive {
				m.player.Stop()
				m.player.ClearPreload()
				m.clearPlaybackTrack()
			}
			m.adjustScroll()
		} else if msg.gen == m.requests.provMutation {
			// Freshest completion failed the mirror check: the queue no longer
			// matches the remote playlist, so disarm remote removes.
			m.providerQueueLen = 0
			m.providerQueueLastPath = ""
		}
		for i := range m.provPane.lists {
			if m.provPane.lists[i].ID == msg.playlistID && m.provPane.lists[i].TrackCount > 0 {
				m.provPane.lists[i].TrackCount--
			}
		}
		return m, nil

	case artistFollowedMsg:
		if msg.gen != m.requests.follow {
			return m, nil
		}
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Follow failed: %s", msg.err)
			return m, nil
		}
		m.setFollowed(followKey("artist", msg.providerName, msg.artistID), msg.follow)
		if msg.follow {
			m.status.Showf(statusTTLDefault, "Following %s", msg.artistName)
		} else {
			m.status.Showf(statusTTLDefault, "Unfollowed %s", msg.artistName)
		}
		// Refresh the nav artist list when it is showing this provider's artists.
		if ab, ok := m.navArtistBrowserFor(msg.providerName); ok {
			return m, fetchNavArtistsCmd(ab, m.nextNavRequest())
		}
		return m, nil

	case playlistFollowedMsg:
		if msg.gen != m.requests.follow {
			return m, nil
		}
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Follow failed: %s", msg.err)
			return m, nil
		}
		m.setFollowed(followKey("playlist", msg.providerName, msg.playlistID), msg.follow)
		if msg.follow {
			m.status.Showf(statusTTLDefault, "Followed playlist %q", msg.playlistName)
		} else {
			m.status.Showf(statusTTLDefault, "Unfollowed playlist %q", msg.playlistName)
		}
		if m.provider != nil && m.provider.Name() == msg.providerName {
			return m, m.fetchProviderPlaylists()
		}
		return m, nil

	case plPickerRemoteMsg:
		if !m.plPicker.visible || m.plPicker.remoteName != msg.providerName || !m.plPicker.remoteLoading {
			return m, nil
		}
		m.plPicker.remoteLoading = false
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "%s playlists unavailable: %s", msg.providerName, msg.err)
			return m, nil
		}
		m.plPicker.remote = filterRemotePickerPlaylists(msg.playlists)
		m.plPickerMaybeAdjustScroll(m.plPickerVisible())
		return m, nil

	case pickerRemoteWriteMsg:
		if msg.err != nil {
			m.status.Showf(statusTTLDefault, "Add failed: %s", msg.err)
			return m, nil
		}
		switch {
		case msg.created && msg.added > 0:
			m.status.Showf(statusTTLBatch, "Created %q & added %d tracks", msg.name, msg.added)
		case msg.created:
			m.status.Showf(statusTTLDefault, "Created %q", msg.name)
		case msg.added > 0 && msg.skipped > 0:
			m.status.Showf(statusTTLBatch, "Added %d to %q, skipped %d duplicates", msg.added, msg.name, msg.skipped)
		case msg.added > 0:
			m.status.Showf(statusTTLDefault, "Added %d to %q", msg.added, msg.name)
		default:
			m.status.Showf(statusTTLDefault, "Nothing added to %q", msg.name)
		}
		if m.provider != nil && m.provider.Name() == msg.providerName {
			return m, m.fetchProviderPlaylists()
		}
		return m, nil

	case provAuthDoneMsg:
		cmd := m.handleProvAuthDone(msg)
		return m, cmd

	case ProvAuthURLMsg:
		if !m.provPane.loading || !m.isActiveProvider(msg.ProviderName) {
			return m, nil
		}
		m.provPane.authURL = msg.URL
		return m, nil

	case devicesListedMsg:
		m.devicePicker.loading = false
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Device list failed: %s", msg.err)
			m.devicePicker.visible = false
		} else {
			m.devicePicker.devices = msg.devices
		}
		return m, nil

	case deviceSwitchedMsg:
		if msg.err != nil {
			m.status.Errorf(statusTTLDefault, "Switch failed: %s", msg.err)
		} else {
			m.status.Showf(statusTTLDefault, "Audio output: %s", msg.name)
			m.audioDevice = msg.name
			_ = m.saveConfigString("audio_device", msg.name)
		}
		// Invalidate cached list so the next open refreshes Active markers.
		m.devicePicker.devices = nil
		return m, nil

	case attachNotifierMsg:
		m.attachNotifier(msg.notifier)
		return m, nil

	case playback.PlayPauseMsg:
		cmd := m.togglePlayPause()
		return m, cmd

	case playback.PlayMsg:
		if !m.player.IsPlaying() || m.player.IsPaused() {
			cmd := m.togglePlayPause()
			return m, cmd
		}
		return m, nil

	case playback.PauseMsg:
		if m.player.IsPlaying() && !m.player.IsPaused() {
			m.togglePlayerPause()
		}
		return m, nil

	case playback.NextMsg:
		cmd := m.skipNext()
		return m, cmd

	case playback.PrevMsg:
		cmd := m.skipPrev()
		return m, cmd

	case playback.SeekMsg:
		cmd := m.seekRelative(msg.Offset, 0)
		return m, cmd

	case playback.SetPositionMsg:
		cmd := m.seekAbsolute(msg.Position)
		return m, cmd

	case playback.SetVolumeMsg:
		m.setVolume(msg.VolumeDB)
		return m, nil

	case playback.SetSpeedMsg:
		m.setSpeed(msg.Ratio)
		return m, nil

	case playback.ToggleMonoMsg:
		m.player.ToggleMono()
		return m, nil

	case playback.StopMsg:
		m.stopByUser()
		return m, nil

	case playback.SetShuffleMsg:
		return m, m.setShuffle(msg.On)

	case playback.SetRepeatMsg:
		return m, m.setRepeat(msg.Mode)

	case playback.EnqueueMsg:
		cmd := m.queueTrackNext(msg.Track)
		return m, cmd

	case playback.PlayTracksMsg:
		cmd := m.playRemoteTracks(msg)
		return m, cmd

	case playback.QuitMsg:
		// Media controls and the signals of headless mode quit like the q
		// key, so the resume position is kept too.
		cmd := m.quit()
		return m, cmd

	case SetEQPresetMsg:
		m.SetEQPreset(msg.Name, msg.Bands)
		m.scheduleEQSave()
		return m, nil

	case SetEQBandMsg:
		m.setCustomEQBand(msg.Band, msg.Gain)
		return m, nil

	case PluginQueueMsg:
		cmd := m.handlePluginQueue(msg)
		return m, cmd

	case pluginQueueAddedMsg:
		cmd := m.appendPluginTracks(msg.tracks...)
		return m, cmd

	case trackFavoriteSyncedMsg:
		m.handleTrackFavoriteSynced(msg)
		return m, nil

	case ShowStatusMsg:
		ttl := statusTTLDefault
		if msg.Duration > 0 {
			ttl = statusTTL(msg.Duration)
		}
		m.status.Show(msg.Text, ttl)
		return m, nil

	case ipcProviderLoadResult:
		cmd := m.handleIPCProviderLoad(msg)
		return m, cmd

	case ipcPlaylistRenamedMsg:
		m.handleIPCPlaylistRenamed(msg)
		return m, nil

	case ipcHistoryClearedMsg:
		cmd := m.handleIPCHistoryCleared(msg)
		return m, cmd

	case ipcFeedLoadResult:
		cmd := m.handleIPCFeedLoad(msg)
		return m, cmd

	case ipcURLLoadResult:
		cmd := m.handleIPCURLResult(msg)
		return m, cmd

	case V2RequestMsg:
		cmd := m.handleV2Request(msg)
		return m, cmd

	case ipcV2ResponseMsg:
		m.handleV2Response(msg)
		return m, nil

	}

	return m, nil
}
