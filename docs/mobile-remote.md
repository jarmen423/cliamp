# Phone Remote

Control cliamp from a phone. cliamp serves a small web app on your network.
The app plays, pauses and seeks, searches Spotify and YouTube, opens your
playlists and edits the queue. The music still plays on the computer: the
phone is a remote, like the Spotify app is for [Spotify Connect](spotify.md#spotify-connect).

The app needs no install. It works in the phone browser, and you can add it
to the home screen. To install it as an app, serve it over `https` (see
[Install it as an app](#install-it-as-an-app)). It works with the TUI and with [headless mode](headless.md),
so a headless cliamp on a home server works well. It runs on Linux, macOS and
[Windows](#on-windows), and one app can switch between all of them.

## Set it up

1. Turn the gateway on in `~/.config/cliamp/config.toml`, then restart cliamp:

   ```toml
   [remote]
   enabled = true
   ```

2. Pair the phone:

   ```sh
   cliamp remote pair
   ```

   It prints a QR code and one URL for each network address of the computer.
   Scan the code with the phone camera, or open a URL on the phone.

The phone and the computer must be on the same network, or on the same
tailnet (see [Away from home](#away-from-home)).

## The app

| Screen | What it does |
|--------|--------------|
| Playing | Art, title and artist, the transport, the volume and the seek bar. The seek bar is drawn from the live spectrum of the music. Drag it to seek. Swipe the art left for the next track and right for the previous one; the mini player swipes the same way. The shuffle button cycles shuffle off, shuffle and Smart Shuffle. Smart Shuffle appears when the queue comes from Spotify |
| Search | Searches Spotify, YouTube, or both at once, in sections: artists, songs, albums and playlists. Tap a song to play it, an artist for their page, an album or playlist to open it. **More songs** loads the next page. The play-next button queues a song after the current track |
| Library | Each provider, viewed as Playlists, Artists, Albums, Songs or Podcasts — the views it can fill. Open a list, then play it from the start or from a track. The header also opens [a Spotify link](#open-a-spotify-link), and Spotify shows the signed-in account with a [switch](#switch-the-spotify-account) |
| Queue | The play-next list and the live playlist. Tap to play a track, or remove it |
| Artist | Tap the artist on Playing, under an album title, or in Search and Library. The artist page shows the photo, the popular tracks with your liked ones marked, the discography by albums, singles and EPs, and compilations, and a Follow button. Spotify only |

The app takes the accent color of the cliamp theme, so it matches the
terminal. Swipe a song in Search, Library and artist pages left to play it next. A mini player sits above the tabs on every screen but Playing.

## Open a Spotify link

Someone sends you a Spotify playlist and you want it on the computer, not
in the Spotify app. Two ways:

- **Share it to cliamp (Android).** The [installed app](#install-it-as-an-app)
  appears in the Android Share sheet as **cliamp**. In Spotify, or anywhere
  a link shows, tap Share, then cliamp. The app opens the playlist, album
  or artist, or plays the song.
- **Paste it.** **Open link** at the top of Library takes a pasted
  `open.spotify.com` link, a `spotify:` URI, or a whole shared message that
  contains one. This is the way on iPhone: iOS does not let web apps into
  its Share sheet.

Playlist, album, artist, song, episode and podcast (show) links all open.
A song or episode plays at once; the rest open as a page with a Play
button. Tapping a Spotify link in another app cannot open cliamp by
itself: only Spotify can claim its own links, so the Share sheet and
pasting are as close as a web app gets.

## Switch the Spotify account

Library shows who Spotify is signed in as. **Switch** signs that account
out and opens the sign-in in the browser **on the computer running
cliamp** — the phone only starts it. Spotify asks which account to use,
so pick the other one there. Playback from Spotify stops while you
switch; the library then shows the new account's playlists. The same
switch exists at the terminal as `cliamp spotify switch`, and
`cliamp spotify whoami` prints the current account.

## Several machines

One app controls every computer that runs cliamp, the way the Spotify app
picks a device. The chip at the top of the Playing screen names the machine
the app controls. Tap it to open the machine list:

- Tap a machine to control it instead.
- **Play here** moves what plays now to that machine, at the same position,
  and pauses the first one. A Spotify or YouTube playlist or album moves as
  the whole list, so the queue goes on there. A local file moves only to a
  machine that has the same file, and a provider track only to a machine
  that has that provider.
- The dot shows whether each machine is online.

To add a machine, run this on it, with the address the app is open at, and
scan the code:

```sh
cliamp remote pair --app https://<machine>.<tailnet>.ts.net:7443
```

The link opens your app and adds the machine; it does not open a second
copy of the app. You can also paste a pairing link of the machine in
**Add a machine**. An app that runs over `https` can only reach machines
that use `https` too, so on each one, run `tailscale serve` as in
[Install it as an app](#install-it-as-an-app) and add `--base` with its
`https` address. The installed app keeps itself on the phone, so it opens
even when the machine you installed it from is off.

## Configuration

```toml
[remote]
enabled = true
listen = ":7700"
name = "Living room"
```

| Key | Default | Meaning |
|-----|---------|---------|
| `enabled` | `false` | Serve the app and the API |
| `listen` | `":7700"` | The address to listen on, as `host:port`. With no host, cliamp listens on every address of the computer |
| `name` | the hostname | The name of this machine in the app's machine list |

## Pairing and security

`cliamp remote pair` makes a pairing token the first time and saves it in
`remote.token` in the config directory. The pairing URL carries the token
after `#t=`. A browser never sends that part of a URL to a server. The app
saves the token and removes it from the address bar.

Anyone with the token can control cliamp, so keep the URL private.

- **Unpair every device:** run `cliamp remote pair --rotate`. A running
  cliamp disconnects the paired devices within a few seconds, and they must
  pair again with the new token.
- **The gateway is plain HTTP.** On a home network that is usually fine. For a
  network you share, listen on the Tailscale address only (see below), or use
  `tailscale serve` for HTTPS.
- Lua plugins cannot write `remote.token`.

## Away from home

With [Tailscale](https://tailscale.com) on the computer and the phone, the
remote works from anywhere on your tailnet. `cliamp remote pair` lists the
Tailscale address with its own URL. To accept connections from the tailnet
only, listen on the Tailscale address:

```toml
[remote]
enabled = true
listen = "100.101.102.103:7700"  # the address from: tailscale ip -4
```

### Install it as an app

Chrome and Safari install a web app as an app only from an `https` page. On
a plain `http` LAN address, "Add to Home screen" makes a shortcut instead.
Tailscale gives the gateway `https` with a real certificate. Turn on HTTPS
certificates for your tailnet in the Tailscale admin console, then serve the
gateway on its own port:

```sh
sudo tailscale serve --bg --https=7443 http://127.0.0.1:7700
cliamp remote pair --base https://<machine>.<tailnet>.ts.net:7443
```

`tailscale serve` connects to `127.0.0.1:7700`, so keep `listen` at `":7700"`
or set it to `"127.0.0.1:7700"`. Scan the code, then use **Install app** in
the browser menu. `sudo tailscale set --operator=$USER` once lets you run
`tailscale serve` without sudo. `--base` pairs through any proxy that
serves the gateway at the root of a URL.

## On Windows

The gateway works the same on Windows. Use the
[release](https://github.com/jarmen423/cliamp/releases/latest)
`cliamp-windows-amd64.exe` from v3.2.0 or later.

1. Add `[remote]` with `enabled = true` to `%APPDATA%\cliamp\config.toml`, then
   start cliamp, as the TUI or as `cliamp --daemon`.
2. The first time, Windows Defender Firewall asks whether `cliamp.exe` may
   use the network. Allow it on **Private networks**. If you missed the
   prompt, allow `cliamp.exe` in **Windows Security > Firewall & network
   protection > Allow an app through firewall**, or allow the port in an
   administrator PowerShell:

   ```powershell
   New-NetFirewallRule -DisplayName "cliamp remote" -Direction Inbound -Protocol TCP -LocalPort 7700 -Action Allow -Profile Private
   ```

   Tailscale traffic does not need the rule when you use `tailscale serve`.
   That command reaches cliamp on `127.0.0.1`.
3. Pair it. To add the Windows machine to an app you already use, give the
   app's address:

   ```powershell
   tailscale serve --bg --https=7443 http://127.0.0.1:7700
   cliamp remote pair --base https://<pc>.<tailnet>.ts.net:7443 --app https://<machine>.<tailnet>.ts.net:7443
   ```

   Scan the code with the phone. The Windows machine then shows in the
   machine list. If `tailscale serve` says access is denied, run it in an
   administrator terminal.

The music plays only while cliamp runs, so keep its terminal open, or start
`cliamp --daemon` at sign-in from Task Scheduler.

### On a desktop browser

The app also works on a computer. Open its `https` address in Chrome or Edge,
for example on the Windows PC, and use **Install** in the address bar. You
then have a small cliamp window that controls any of your machines, such as
the headless cliamp on a home server.

## For client developers

The app is one client of an open protocol. A native app can use the same
endpoints:

| Endpoint | What it serves |
|----------|----------------|
| `GET /` | The web app |
| `GET /v2` | A WebSocket that carries [V2 IPC](remote-control.md#version-2), one JSON line per message |
| `GET /info` | `{"name": …, "version": …}`: the machine name and the cliamp version |
| `GET /art/<sha256>.<ext>` | Album art that cliamp read from a local file. A track's `album_art_url` names the file. Provider art is a public `https` URL |

`/v2`, `/info` and `/art` need the token. `/info` and `/art` allow any
origin (CORS), so an app served by one machine can show another. Send it as `Authorization: Bearer <token>`.
A browser cannot set headers on a WebSocket, so it can offer the
subprotocols `cliamp.v2` and `cliamp.token.<token>` instead. cliamp answers
with `cliamp.v2`.

A WebSocket that sends `subscribe` gets events and reads no more requests,
the same as the Unix socket. So a client opens two: one for requests and one
for events. A request socket that sends nothing for 60 seconds is closed, so
send a request, such as `capabilities`, every 25 seconds. The app does all of
this in `remote/web/core/`, which uses no DOM APIs.

## Troubleshooting

- **The phone cannot load the page.** Check that cliamp runs and that
  `enabled = true` is in `[remote]`. Check the firewall of the computer for
  the port, by default 7700. On a guest Wi-Fi network, devices often cannot
  reach each other.
- **"cliamp no longer accepts this phone's pairing."** The token changed. Run
  `cliamp remote pair` and scan the new code.
- **`remote: listen: address already in use`.** Another program uses the
  port. Set another one in `listen`.
