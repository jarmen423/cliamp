# Remote Control (IPC)

Control cliamp locally from a terminal, script, status bar, or GUI.

cliamp listens on `~/.config/cliamp/cliamp.sock` with `0600` permissions. It
uses newline-delimited JSON over a local Unix socket. To use SSH, run the client
command on the host that owns the socket.

The socket path can have at most 107 bytes on Linux and Windows and 103 bytes
on macOS. When the path is longer, cliamp and its client commands stop with
`socket path is too long`. Set `CLIAMP_CONFIG_DIR` to a shorter directory.

## Quick Start

```sh
cliamp status --json
cliamp next
cliamp remote state
cliamp remote events runtime.state runtime.job
cliamp remote pair              # pair a phone with the network gateway
cliamp remote pair --rotate     # unpair every device
```

`cliamp status --json` always includes `position`, `volume` and `index`,
because 0 is a valid value of each. It leaves out other fields that have no
value. `index` is `-1` when the playlist is empty.

IPC supports version 2 only. Clients must send a V2 envelope with each request.
See [Upgrading IPC Clients To V2](upgrading-ipc-v2.md) when you migrate a raw
socket integration.

## Version 2

V2 responses use the request `id` and always include `"version":2`.

```json
{"version":2,"id":"state","method":"state.get"}
{"version":2,"id":"play","method":"operation.submit","operation":"play","params":{}}
{"version":2,"id":"queue","method":"operation.submit","operation":"queue.enqueue","params":{"index":4,"if_revision":18}}
```

Use these methods:

| Method | Purpose |
| --- | --- |
| `capabilities` | List available operation names and parameter hints |
| `state.get` | Read the runtime snapshot |
| `spectrum.get` | Read current visualizer bands |
| `operation.submit` | Start a runtime or library operation |
| `job.get` | Read an operation job by `job_id` |
| `job.cancel` | Request cancellation of an active job |
| `subscribe` | Start a server-to-client event stream |

`operation.submit` returns a job immediately. A job can become `queued`,
`running`, `succeeded`, `failed`, or `canceled`. Its final record contains the
operation result and the snapshot from the committed operation.

```json
{
  "version": 2,
  "id": "play",
  "ok": true,
  "job": {
    "id": "8f0d...",
    "operation": "play",
    "state": "queued"
  }
}
```

Fast operations can finish before the client requests `job.get`. Slow work,
such as provider access, URL resolution, downloads, lyrics, and saved playlist
writes, remains asynchronous.

## Runtime Snapshot

`state.get` returns a snapshot with the active audio track, logical playlist
track, playback state, position, duration, seekability, modes, EQ, visualizer,
theme, stream error, and two revisions. `playlist` names the loaded saved
playlist. After `provider.load` or `provider.load_album` of another list,
`playlist` holds the provider key and the ID, for example `navidrome:42` or
`navidrome:album:7`. `device` names the output device that the last `device`
operation reported. `system_volume` is the output volume of the operating
system in percent, from 0 to 100, and `system_muted` its mute. Both are absent
when cliamp cannot control the system volume or has not read it yet. See
[System volume](#system-volume).

```json
{
  "version": 2,
  "id": "state",
  "ok": true,
  "snapshot": {
    "revision": 18,
    "playlist_revision": 7,
    "state": "playing",
    "track": {"title":"Song","path":"/music/song.flac"},
    "logical_track": {"title":"Song","path":"/music/song.flac"},
    "position": 42.5,
    "duration": 183,
    "seekable": true,
    "play_next_total": 2
  }
}
```

`revision` changes when meaningful runtime state changes. `playlist_revision`
changes when the live playlist or play-next list changes. Position-only playback
ticks do not create events. Send `if_revision` with destructive live-playlist or
play-next operations to reject stale GUI actions with the `conflict` error code.

`track` keeps `provider_meta`, embedded playback flags, and directory-source
state. A GUI can send a provider result through `track.play`,
`track.queue`, `playlist.add`, `playlist.add_many`, or `playlist.replace`
without losing provider identity. When cliamp saves such a track to a local
playlist, Favorites or Recently Played, it keeps only the `provider_meta` keys
that hold letters, digits, `.`, `_` and `-`.

The `bookmark` field of `track` keeps its name for existing scripts. It reports
the favorite ♥ state of the track, the same state as the playlist row marker.
`cliamp status --json` shows it for the current track. cliamp ignores the field
when a client sends a track, so use `playlist.bookmark` to change a favorite.

## Operations

Run `cliamp remote capabilities` to get the current machine-readable list.

| Group | Operations |
| --- | --- |
| Playback | `play`, `pause`, `toggle`, `stop`, `quit`, `next`, `prev`, `volume`, `volume.adjust`, `seek`, `seek.absolute`, `speed`, `speed.adjust`, `shuffle`, `repeat`, `mono`, `eq`, `device` |
| System | `system.volume`, `system.mute` |
| Appearance | `theme`, `vis` |
| Live playlist | `queue`, `queue.list`, `queue.play`, `queue.enqueue`, `queue.remove`, `queue.move`, `queue.clear`, `track.play`, `track.queue` |
| Play-next | `playnext.list`, `playnext.remove`, `playnext.move`, `playnext.clear` |
| Sources | `load`, `url.load`, `save`, `lyrics`, `history`, `history.clear` |
| Providers | `provider.list`, `provider.playlists`, `provider.tracks`, `provider.load`, `provider.search`, `provider.artists`, `provider.artist_albums`, `provider.albums`, `provider.album_tracks`, `provider.load_album`, `provider.artist`, `provider.load_artist`, `provider.favorite`, `provider.catalog`, `provider.open_link`, `provider.account`, `provider.switch_account`, `provider.follow_artist` |
| Saved playlists | `playlist.create`, `playlist.rename`, `playlist.delete`, `playlist.add`, `playlist.add_many`, `playlist.replace`, `playlist.remove`, `playlist.bookmark` |
| Plugins | `plugin.call`, `plugin.commands` |

`queue.*` applies to the live playlist. `playnext.*` applies only to the
play-next list. They use separate zero-based indexes.

`queue`, `queue.move`, and `queue.remove` follow the rules of the
`Shift+Up`, `Shift+Down`, and `x` keys. The Lua `cliamp.queue` functions
follow the same rules:

- `queue.move` swaps the tracks at `index` and `to`. While shuffle is on,
  it fails with `conflict` and changes nothing.
- When the live playlist mirrors a saved local playlist, `queue.move` saves
  the new order to that playlist. `queue.remove` removes the track from it
  too. When that save fails, the edit fails with `internal_error` and
  changes nothing. Favorites is not a playlist file, so an edit of a loaded
  Favorites list changes only the live playlist.
- `queue`, `queue.move`, and `queue.remove` record no undo. After one of
  them, `Ctrl+Z` does not undo the last TUI edit, because that undo would
  drop the new change.
- While the live playlist mirrors a saved local playlist, `queue.remove`
  fails with `conflict` for a track that a directory source of that playlist
  supplies. A removal of the playing track stops playback.
- `queue` appends a track. The live playlist then mirrors no saved playlist.
- When an edit changes the next track, cliamp re-arms the gapless preload.

`vis` with the name `list` returns every mode in the order of the `v` key
cycle: the built-in modes, then the visualizers of Lua plugins. `cliamp vis
list` prints the same list when cliamp runs.

The `list` result also names the active mode in `visualizer` and gives its
zero-based position in `items` as `index`. The result leaves out `index` when
it is 0. A Lua visualizer can have the name of a built-in mode, so use `index`
to find the active mode. `cliamp vis list` marks only that row.

```sh
cliamp remote call vis --params '{"name":"list"}' --wait
```

`theme list` and `cliamp theme list` return `Default - Terminal colors` as
the first item. `theme` accepts that name or `default` to select the terminal
colors.

`eq` takes a built-in preset name, such as `Rock`, or a `band` and a `value`.
It also accepts `Custom` to restore the saved custom curve. An unknown
preset name fails the job and does not change the EQ.

`shuffle` and `mono` take the `name` `on`, `off` or `toggle`. `shuffle` also
takes `smart`, which turns on Smart Shuffle and shuffle with it. It fails with
`unavailable` when the queue's provider cannot recommend songs. `on` is plain
shuffle and turns Smart Shuffle off, `off` turns both off, and `toggle` flips
shuffle and Smart Shuffle with it. The snapshot reports `smart_shuffle`, and
`smart_available` when the queue can start it. `repeat` takes
`off`, `all`, `one` or `cycle`. With no `name`, they toggle or cycle. Any
other name fails the job with `invalid_params` and changes nothing.

### System volume

`system.volume` and `system.mute` change the output volume of the operating
system on the machine that runs cliamp, not cliamp's own `volume` in dB.
`system.volume` takes a `value` from 0 to 100 percent and clamps any other
number. `system.mute` takes the `name` `on`, `off` or `toggle`, and toggles
with no `name`. Both jobs return `system_volume` and `system_muted` as the
system reports them after the change. A failed change fails the job with
`internal_error` and the reason in `detail`.

```sh
cliamp remote call system.volume --params '{"value":40}' --wait
cliamp remote call system.mute --params '{"name":"toggle"}' --wait
```

cliamp reads the system volume at start and after each change. While a
client subscribes to events, it reads it again every 3 seconds, so a change
made on the computer reaches the client within a few seconds and publishes a
`runtime.state` event. Headless mode supports both operations.

| Platform | Requirement |
| --- | --- |
| Linux | `wpctl` (PipeWire), `pactl` (PulseAudio) or `amixer` (ALSA), tried in that order. cliamp drives the default output and caps it at 100% |
| macOS | Built in, through `osascript`. An output with no volume control, such as some HDMI and USB devices, fails the job |
| Windows | Built in, through Core Audio |

`cliamp remote capabilities` lists the two operations only when cliamp can
control the system volume. Elsewhere they are absent.

`playlist.bookmark` keeps its name for existing scripts. It toggles the favorite
♥ of `track`, as `f` does in the TUI. It needs a known `provider` key, but it
does not change the playlist. A radio station toggles its station favorite,
and its `bookmark` field in provider lists changes too. A station
that is a row of a loaded saved playlist toggles the favorites store instead.
The `bookmark` field of that row in `queue.list` changes. cliamp matches the
row by path.

`provider.artist` returns an artist profile, the data of the TUI artist
screen: `artists` holds the artist with `image_url`, `followers` and
`genres`, `tracks` holds the popular tracks, and `albums` holds the
discography, each release with `image_url` and a `type` of `album`,
`single` or `compilation`. Pass the artist `id` in `artist`, or a `track`
to open the artist of that track. `provider.load_artist` loads the popular
tracks as the live playlist and plays the first. `provider.list` reports
`artist_profiles` for the providers that support both; today that is
Spotify.

On its first page (`offset` 0), `provider.search` on a provider with
multi-type search also returns the matched `artists`, `albums` and
`playlists`, next to `tracks`; later pages extend `tracks` only.
`provider.follow_artist` follows (`"follow": true`) or unfollows an
`artist`.

`provider.open_link` resolves a provider share link passed in `path` —
a URL, a URI, or shared text that contains one — and returns `link`:
a `kind` of `playlist`, `album` or `artist` with the `id` to open, or
`track` / `episode` with the full `track` to play, plus a best-effort
`name` and `image_url`. A podcast show link opens as its episode
playlist. `provider.account` returns the signed-in `account`, and
`provider.switch_account` signs out and runs the interactive sign-in
again — blocking for the browser flow on the cliamp machine — then
returns the new account. Switching stops playback from that provider.
`provider.list` reports `links` and `account` for the providers that
support them; today that is Spotify.

Use IDs returned by `provider.playlists` for subsequent provider operations.
Radio favorite IDs are stable `f:<station URL>` values, not positional
`f:<index>` values.

Request provider list responses with `offset` and `limit` when the provider
supports paging. Use `playlist.replace` to save a GUI-created order, sort, or
deduplication result as one operation when the provider supports playlist saving.

## Events

Subscribe with an exact topic list. The acknowledgement is V2. Later lines use
the shared event envelope, so plugin and runtime events use the same form.

```json
{"version":2,"id":"events","method":"subscribe","topics":["runtime.state","runtime.job"]}
```

Core retained topics are `runtime.state`, `runtime.playback`,
`runtime.playlist`, and `runtime.settings`. `runtime.job` is temporary and
contains final job records. Plugin topics keep their `plugin.*` names.

If a client cannot keep up, it receives `system.overflow` with
`{"resync_required":true}` before the stream closes. Reconnect and request
`state.get` before you accept more changes.

## Spectrum Stream

`cliamp visstream` uses V2 `spectrum.get` internally. It outputs one plain
NDJSON frame at 30 FPS by default for status-bar and visualizer integrations.
Use `--fps` to set a rate from 1 through 60. GUI clients can request one current
frame with `spectrum.get`.

## CLI V2 Client

```sh
cliamp remote state
cliamp remote capabilities
cliamp remote call queue.enqueue --params '{"index":4,"if_revision":18}' --wait
cliamp remote job JOB_ID
cliamp remote cancel JOB_ID
cliamp remote events runtime.state runtime.job
```

`remote call` prints the V2 response as JSON and can submit every listed
operation. Use it in scripts and to validate a GUI integration.

Named CLI commands such as `cliamp volume`, `cliamp seek`, `cliamp load`, and
`cliamp plugins call` use V2 jobs internally. `volume` sets an absolute dB
value. `seek` is relative to the current position. V2 subscriptions read
`plugin.*` topics and runtime events.

## Network Clients

The `[remote]` gateway carries V2 over a WebSocket on `/v2`, one JSON line per
message, for phones and other devices on the network. The same operations,
jobs and events work there. `cliamp remote pair` prints the pairing URL and
token. See [Phone Remote](mobile-remote.md#for-client-developers).

## Headless Mode

```sh
cliamp --daemon --auto-play --playlist Lofi
```

Headless mode runs the same player as the TUI with no screen. It exposes the
same playback, queue, provider, saved-playlist, plugin, job, snapshot, and event
APIs. It loads Lua plugins, reports plays to the providers, and records Recently
Played when a track starts. It has no theme or visualizer selection, so `theme`
and `vis` are not in `capabilities`. Use `capabilities` instead of assuming that
each interactive-only operation is available.

## Errors And Limits

V2 errors use stable codes: `invalid_version`, `invalid_request`,
`invalid_params`, `unknown_operation`, `not_found`, `conflict`, `unavailable`,
`canceled`, and `internal_error`. Runtime failures can include a `detail` string
with a provider, device, or plugin diagnostic.

Frames are limited to 1 MiB. Use paging for large provider or playlist results.
Jobs are process-local, bounded, and retained for 15 minutes after completion.
cliamp cancels them during an orderly server shutdown. They are not available
after a restart.
