# Changelog

The changes of the jarmen423 fork of cliamp. Each GitHub
[release](https://github.com/jarmen423/cliamp/releases) also lists its
commits. This file starts at v3.2.0.

## v3.3.0 (2026-11-12)

### Phone remote

- **System volume:** a second slider on Playing sets the output volume of
  the computer, with a mute button, beside cliamp's own volume. It works on
  Linux with `wpctl`, `pactl` or `amixer`, on macOS and on Windows. A change
  made on the computer shows on the phone within a few seconds.
- **Open a Spotify link:** share a link to the installed app on Android, or
  paste one into **Open link** in Library. Playlist, album, artist, song,
  episode and podcast links all open.
- **Switch the Spotify account:** Library shows the signed-in account, and
  **Switch** starts a new sign-in on the computer.
- **Swipe:** swipe the art left or right to skip, and swipe a song in a
  list left to play it next.
- **Smart Shuffle:** the shuffle button cycles off, shuffle and Smart
  Shuffle when the queue comes from Spotify.
- **Search** shows artists, songs, albums and playlists in sections, with
  more songs on demand.
- **Library** views each provider as Playlists, Artists, Albums, Songs or
  Podcasts.
- **Artist pages** have a Follow button and open from Search and Library.
- The Playing screen fits short phones, so the volume stays in view.

### Spotify

- `cliamp spotify whoami` prints the signed-in account, and
  `cliamp spotify switch` signs in with another one.
- Saved shows list under Podcasts in the library.

### Protocol

- New operations: `system.volume` and `system.mute`. The snapshot carries
  `system_volume` and `system_muted`. Both operations are absent where
  cliamp cannot control the system volume.
- New operations: `provider.open_link`, `provider.account`,
  `provider.switch_account` and `provider.follow_artist`.
- `shuffle` takes the name `smart`. The snapshot carries `smart_shuffle`
  and `smart_available`.

## v3.2.0 (2026-11-06)

### Phone remote

cliamp serves a web app that controls it from a phone, on your LAN or
tailnet. See [docs/mobile-remote.md](docs/mobile-remote.md).

- Turn it on with `[remote] enabled = true`, then run `cliamp remote pair`
  and scan the QR code. The link carries a pairing token, which the phone
  keeps. `cliamp remote pair --rotate` unpairs every device and disconnects
  the connected ones.
- **Playing:** art, transport, volume, shuffle and repeat, and a seek bar
  drawn from the live spectrum.
- **Search:** Spotify, YouTube, or both at once. Spotify albums open as
  albums.
- **Library:** the playlists of each provider, played from any track.
- **Queue:** the play-next list and the playlist. Tap to play, or remove.
- **Artist pages:** a photo header, the popular tracks with your liked ones
  marked, and the discography by albums, singles and EPs, and compilations.
  Spotify only.
- **Several machines:** one app controls every computer that runs cliamp.
  Pick one from the chip on Playing. **Play here** moves the track, its
  playlist or album, and the position to another machine.
  `cliamp remote pair --app <url>` adds a machine to the app you already use.
- **Install as an app:** serve the gateway over HTTPS with
  `tailscale serve` and pair with `cliamp remote pair --base <url>`. The app
  then installs on Android, iOS, and in Chrome or Edge on a desktop.
- **Windows:** the gateway runs in `cliamp.exe` too. The guide covers the
  firewall rule and `tailscale serve` on Windows.
- New config keys: `[remote] enabled`, `listen` (default `":7700"`), and
  `name` (default: the hostname).

### Protocol

- The gateway carries V2 IPC over a WebSocket on `/v2`. `/info` names the
  machine, and `/art` serves local album art. All three need the token.
- New operations: `provider.artist` returns an artist's details, popular
  tracks and discography. `provider.load_artist` loads the popular tracks as
  the playlist.
- `provider.list` reports `artist_profiles`. Albums carry `image_url` and
  `type`.
- `ipc.Server.ServeConn` serves V2 on any `net.Conn`.
- Lua plugins cannot write the pairing token.
