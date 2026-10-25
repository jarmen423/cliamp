# cliamp (jarmen423 fork)

This is my fork of [bjarneo/cliamp](https://github.com/bjarneo/cliamp), the retro
terminal music player inspired by Winamp. cliamp is bjarneo's project; this fork
tracks upstream and adds the features below. Releases from this fork are at
[jarmen423/cliamp/releases](https://github.com/jarmen423/cliamp/releases).

cliamp plays local files, streams, podcasts, YouTube, YouTube Music, SoundCloud, Mixcloud, Bilibili, Spotify, Qobuz, Tidal, NetEase Cloud Music, Yandex Music, Xiaoyuzhou (小宇宙), Navidrome, Lyrion, Plex, Jellyfin, Emby, and Audiobookshelf. It has a spectrum visualizer, parametric EQ, and playlists.

cliamp uses [Bubbletea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), [Beep](https://github.com/gopxl/beep), and [go-librespot](https://github.com/devgianlu/go-librespot).

https://github.com/user-attachments/assets/55e251f2-e13b-43d9-bb6a-7b1960e7d7d0

## What this fork adds

- **Immersive mode (prototype).** A browse layout like the Spotify apps:
  visualizer band, nav pills, a Now Playing and Queue column, and a list, rows
  or grid canvas with cover art. Press `I`, or set `immersive = true` in
  `config.toml`. See [Immersive mode](docs/keybindings.md#immersive-mode-prototype).
- **Pixel visualizers.** Aurora and Phosphor, plus the Ridgeline,
  EventHorizon, Stage and Encore scenes. Encore looks out from the stage at an
  arena of phone lights that react to the music.
- **Terminal graphics.** Cover art and the pixel visualizers render as real
  images over kitty graphics or Sixel (kitty, Ghostty, Windows Terminal,
  WezTerm, foot, Konsole). They show in immersive mode and in the full-screen
  visualizer (`V`). The classic layout, and terminals without graphics
  support, use half-block text. See the `images` setting in
  [docs/configuration.md](docs/configuration.md).
- **Spotify.** A [Browse](docs/spotify.md#browse) page for genres, moods and
  new releases; a library browser and artist pages; smart shuffle; playlist
  write operations; and [Spotify Connect](docs/spotify.md#spotify-connect)
  receiver mode, so the Spotify apps can play through cliamp.
- **Radio.** Song, artist, album and playlist radio seeded from Spotify
  stations, opened as a playlist without starting playback. When Spotify has
  no station for a track, cliamp says so.
- **Resume.** cliamp reopens the last session for every provider, including the
  queue and the page you were on.
- **Mouse.** Click the timeline to seek, including in the fullscreen
  visualizer, and right-click a track for a context menu.
- **Windows.** A single static `cliamp.exe` with Spotify support, credentials
  stored with DPAPI, Spotify sign-in over a free local port, and Sixel
  graphics in Windows Terminal.

Upstream links: [cliamp.stream](https://cliamp.stream) (upstream's site),
[hosted docs](https://whiterose.org.contextowl.co/docs/cliamp),
[cliamp mobile](https://github.com/cliamp/cliamp-mobile) for Android, and the
[Discord](https://discord.gg/4VpCzXPuj2). They describe upstream cliamp, so the
fork's features above are not in them.

## Install

macOS, Linux, and Windows (Git Bash):

```sh
curl -fsSL https://raw.githubusercontent.com/jarmen423/cliamp/main/site/install.sh | sh
```

The script downloads the latest release of this fork, checks it against the
release's `checksums.txt`, and installs it to `~/.local/bin` (or
`/usr/local/bin` when `~/.local/bin` is not on `PATH`). Set `INSTALL_DIR` to
choose another directory.

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/jarmen423/cliamp/main/site/install.ps1 | iex
```

The PowerShell script downloads `cliamp-windows-amd64.exe`, checks it against
`checksums.txt`, and installs it to `%USERPROFILE%\.local\bin` when that is on
`PATH`. Otherwise it installs to `%LOCALAPPDATA%\Programs\cliamp` and adds that
directory to your user `PATH`. Set `$env:INSTALL_DIR` first to choose another
directory. On Windows on ARM it installs the x64 build, which runs under
emulation.

The `cliamp.stream/install.sh` script, Homebrew (`bjarneo/cliamp/cliamp`), the
AUR package, `nix run github:bjarneo/cliamp` and
`go install github.com/bjarneo/cliamp` all install upstream cliamp, without this
fork's features.

> **Upgrading:** `cliamp upgrade` downloads upstream's latest release and
> replaces this fork with it, even when upstream's version number is lower. To
> update the fork, run the install script again.

**Pre-built binaries**

Download the file for your platform from
[Releases](https://github.com/jarmen423/cliamp/releases/latest):
`cliamp-linux-amd64`, `cliamp-linux-arm64`, `cliamp-darwin-amd64`,
`cliamp-darwin-arm64`, `cliamp-windows-amd64.exe`, or
`cliamp-windows-amd64.zip`. On macOS and Linux, make the file executable and
move it onto your `PATH`:

```sh
chmod +x cliamp-linux-amd64
mv cliamp-linux-amd64 ~/.local/bin/cliamp
```

> **macOS:** Pre-built binaries link dynamically to FLAC, Vorbis, Ogg, and mpg123
> from Homebrew. If you download from Releases or use `install.sh`, install these
> libraries first. Otherwise, you can see errors such as
> `Library not loaded: /opt/homebrew/opt/libvorbis/lib/libvorbisenc.2.dylib`:
>
> ```sh
> brew install flac libvorbis libogg mpg123
> ```
>
> **Linux:** Pre-built binaries link FLAC, Vorbis, Ogg, and mpg123 statically. No
> extra codec packages are required. You can still need an ALSA bridge for your
> sound server. See [Troubleshooting](#troubleshooting).
>
> **Windows:** `cliamp-windows-amd64.exe` is a single file that imports only
> Windows system DLLs, Spotify included. `cliamp-windows-amd64.zip` holds the
> same program. On Windows, cliamp stores its config under `%APPDATA%\cliamp`
> unless `HOME` names another directory.

**Optional runtime dependencies** for all platforms and install methods:

- [ffmpeg](https://ffmpeg.org/) for AAC, ALAC, Opus, and WMA playback
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) for YouTube, YouTube Music, SoundCloud, Mixcloud, Bandcamp, Bilibili, and NetEase Cloud Music

On macOS, run `brew install ffmpeg yt-dlp`. On Linux, use your distribution package manager.

On Windows, install `ffmpeg` and `yt-dlp` with your package manager. Keep both on `PATH`.

**Build from source**

```sh
git clone https://github.com/jarmen423/cliamp.git && cd cliamp && go build -o cliamp .
```

Install ALSA development headers before you build on Linux. See
[Building from source](#building-from-source).

## Quick Start

```sh
cliamp ~/Music                     # play a directory
cliamp *.mp3 *.flac               # play files
cliamp https://example.com/stream  # play a URL
```

Press `Ctrl+K` to see all keybindings.

**Configure remote providers** such as Navidrome, Lyrion, Plex, Jellyfin, Emby, Audiobookshelf, Spotify, Qobuz, Tidal, Mixcloud, YouTube Music, and NetEase Cloud Music with the interactive wizard:

```sh
cliamp setup
```

The wizard guides you through each provider. It writes the provider keys into the section of that provider in your config file (`~/.config/cliamp/config.toml`, or `%APPDATA%\cliamp\config.toml` on Windows unless `HOME` names another directory). It keeps your other keys and comments. It validates supported server connections during setup. The OAuth providers Spotify, Qobuz and Tidal sign in later in the player. Mixcloud checks optional browser-session or OAuth credentials when you use them. See [docs/cli.md](docs/cli.md#setup-wizard) for details.

See the [Mixcloud provider guide](docs/mixcloud.md) for discovery, account,
creator/show, genre search, local genre favorites, authentication, signed-in
playback, resume, seeking, and limitations.

For podcast discovery and subscriptions, run `cliamp --provider podcast`.
Browse Apple's top 100 shows and 19 categories, search with `/` then `Enter`,
and subscribe with `f`. No account or API key is needed.
See the [Podcasts guide](docs/podcasts.md).

## Remote Control

Control a running cliamp from a script, a status bar, or a key binding:

```sh
cliamp toggle
cliamp next
cliamp status --json
```

Media keys work through MPRIS on Linux, Now Playing on macOS, and global
hotkeys on Windows.

Run `cliamp --daemon` to play without a TUI. Headless mode runs the same
player as the TUI. It loads your Lua plugins, reports plays to Navidrome,
Jellyfin, and the other providers that take play reports, and adds each track
to Recently Played when the track starts. See
[docs/headless.md](docs/headless.md) and
[docs/remote-control.md](docs/remote-control.md).

## Radio

Press `R` in the player to browse about 58,000 online radio stations in the [Radio Browser](https://www.radio-browser.info/) directory.

Cut that down by location or genre: `N` browses by country, **Browse genres &
tags** opens the directory's complete tag index, and `/` filters either list.
`f` pins the countries you listen to, and `Enter` on a country or tag loads its
stations as a playlist. cliamp does not work out where you are unless you pick
"Use my location" and accept; it then reads your country from the system
timezone, never from a geo-IP service. See [docs/radio.md](docs/radio.md).

Add your own stations to `~/.config/cliamp/radios.toml` (or `%APPDATA%\cliamp\radios.toml` on Windows unless `HOME` names another directory). See [docs/configuration.md](docs/configuration.md#custom-radio-stations).

To host a radio station, use [cliamp-server](https://github.com/bjarneo/cliamp-server).

## Building from source

**Prerequisites:**

- [Go](https://go.dev/dl/) 1.26.10 or later
- ALSA development headers (Linux only, required by the audio backend)

**Linux (Debian/Ubuntu):**

```sh
sudo apt install libasound2-dev libflac-dev libvorbis-dev libogg-dev libmpg123-dev
```

**Linux (Fedora):**

```sh
sudo dnf install alsa-lib-devel flac-devel libvorbis-devel libogg-devel mpg123-devel
```

**Linux (Arch):**

```sh
sudo pacman -S alsa-lib flac libvorbis libogg mpg123
```

**macOS:**

```sh
brew install flac libvorbis libogg mpg123 pkg-config
```

**Windows:** The core player needs no extra SDKs. It uses pure-Go audio decoding. `ffmpeg.exe` and `yt-dlp.exe` remain optional runtime dependencies for the same formats and providers as other platforms.

Spotify support uses `go-librespot`. It needs CGO and a MinGW toolchain, and links every non-system library statically into a single `cliamp.exe`:

1. Install [MSYS2](https://www.msys2.org/).
2. Open the **MSYS2 MinGW64** terminal, not the standard MSYS2 terminal. Install the toolchain and codec libraries:

   ```sh
   pacman -S make \
     mingw-w64-x86_64-gcc mingw-w64-x86_64-go mingw-w64-x86_64-pkg-config \
     mingw-w64-x86_64-libogg mingw-w64-x86_64-libvorbis \
     mingw-w64-x86_64-flac mingw-w64-x86_64-mpg123
   ```

   To check if Go was installed correctly, run

   ```sh
   go env GOROOT
   ```

   If the command causes the error `go: cannot find GOROOT directory: 'go' binary is trimmed and GOROOT is not set`,
   it means that the enviroment `GOROOT` variable has to be manually set:

   ```sh
   export GOROOT=/mingw64/lib/go
   ```

   Now, when running `go env GOROOT`, the output should be: `<mtsys2-install-folder>/mingw64/lib/go`
3. In that MinGW64 terminal, build with CGO enabled and static linking. This keeps `gcc` and `pkg-config` on `PATH`:

   ```sh
   CGO_CFLAGS="-DFLAC__NO_DLL" CGO_LDFLAGS="-static -lshlwapi" CGO_ENABLED=1 go build -o cliamp.exe .
   ```

   `-static` alone is not enough on MinGW: `-DFLAC__NO_DLL` stops FLAC's headers declaring `dllimport` references that `libFLAC.a` cannot satisfy, and `-lshlwapi` covers `libmpg123.a`'s Windows system dependency. If linking still fails with undefined references from one codec library, link just that library statically (`-Wl,-Bstatic -l<lib> -Wl,-Bdynamic` in `CGO_LDFLAGS`).

4. The finished `cliamp.exe` imports only Windows system DLLs. Copy it anywhere and run it — no MinGW or codec DLLs, and no `PATH` changes, are needed on the machine that runs it.

**Clone and build:**

```sh
git clone https://github.com/jarmen423/cliamp.git
cd cliamp
make && make install
```

Or without Make: `go build -o cliamp .`

`make install` places the binary in `~/.local/bin/`.

A source build uses the same optional runtime dependencies as the other install methods. See [Install](#install).

## Docs

The docs for this fork are in [docs/](docs/). Upstream's hosted docs at
[whiterose.org.contextowl.co/docs/cliamp](https://whiterose.org.contextowl.co/docs/cliamp)
cover upstream cliamp only.

## Troubleshooting

**No audio output**

cliamp reports `audio output unavailable` when it cannot open an output
device. On Linux systems that use PipeWire or PulseAudio, the cliamp ALSA
backend needs a bridge package to route audio through the sound server:

- **PipeWire:** `pipewire-alsa`
- **PulseAudio:** `pulseaudio-alsa` (`libasound2-plugins` on Debian/Ubuntu)

Install the package for your system:

```sh
# PipeWire (Arch)
sudo pacman -S pipewire-alsa

# PulseAudio (Arch)
sudo pacman -S pulseaudio-alsa

# Debian/Ubuntu (PipeWire)
sudo apt install pipewire-alsa

# Debian/Ubuntu (PulseAudio, including WSL2)
sudo apt install libasound2-plugins
```

On WSL2 see [WSL2 setup](docs/configuration.md#wsl2-windows-subsystem-for-linux)
for the extra ALSA routing step.

## Credits

cliamp is created by bjarneo ([GitHub](https://github.com/bjarneo),
[x.com/iamdothash](https://x.com/iamdothash)). This fork is maintained by
[jarmen423](https://github.com/jarmen423). Report problems with the fork's
features in [jarmen423/cliamp issues](https://github.com/jarmen423/cliamp/issues),
not upstream.

## Disclaimer

Use this software at your own risk. The authors are not responsible for damage or issues that result from its use.
