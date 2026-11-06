# Phone Remote

Control cliamp from a phone. cliamp serves a small web app on your network.
The app plays, pauses and seeks, searches Spotify and YouTube, opens your
playlists and edits the queue. The music still plays on the computer: the
phone is a remote, like the Spotify app is for [Spotify Connect](spotify.md#spotify-connect).

The app needs no install. It works in the phone browser, and you can add it
to the home screen. To install it as an app, serve it over `https` (see
[Install it as an app](#install-it-as-an-app)). It works with the TUI and with [headless mode](headless.md),
so a headless cliamp on a home server works well.

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
| Playing | Art, title and artist, the transport, the volume and the seek bar. The seek bar is drawn from the live spectrum of the music. Drag it to seek |
| Search | Searches Spotify, YouTube, or both at once. Tap a result to play it. The play-next button queues it after the current track |
| Library | The playlists of each provider. Open one, then play it from the start or from a track |
| Queue | The play-next list and the live playlist. Tap to play a track, or remove it |

The app takes the accent color of the cliamp theme, so it matches the
terminal. A mini player sits above the tabs on every screen but Playing.

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
