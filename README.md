# YellowJacket

*Music how it was meant to bee.*

YellowJacket is a fast, cross-platform desktop music player for your local
collection. It plays your files, keeps your library tidy, and helps you discover
and organize your music — all in a clean, responsive interface. No accounts, no
streaming, no telemetry: just your music on your machine.

Runs on **Linux**, **macOS**, and **Windows**.

## Features

### Play your music
- Plays **MP3, FLAC, OGG Vorbis, and WAV**
- Play, pause, seek, and volume control with a mute toggle
- Gapless, glitch-free seeking backed by a read-ahead buffer
- A queue you can add to, reorder, and shuffle, with play-next support
- Shuffle and repeat (off / all / one)
- Picks up right where you left off — remembers your track, position, and volume between sessions
- Media-key and MPRIS support on Linux, so your desktop's playback controls just work

### Keep your library organized
- Point it at your music folders and it scans them automatically
- Reads tags and embedded cover art, and de-duplicates artwork so it isn't stored twice
- Incremental sync — only new or changed files get reprocessed, and deleted files are cleaned up
- Browse by **album**, **artist**, or **genre**, or search across everything
- Mark favorites and see what you've been listening to with play history
- Edit track tags directly when something's off

### Playlists
- Create playlists, drag tracks in, and reorder them
- **Smart playlists** that build themselves from rules (by genre, rating, play count, and more)
- Pin a default playlist and spot duplicate tracks at a glance

### Discover and clean up (powered by MusicBrainz)
- **Explore** — browse artists, releases, and genres from the MusicBrainz catalog, not just what's already in your library
- **Auto-tag** — match your files against MusicBrainz to fill in correct artist, album, and track metadata, with a review step before anything is written
- **Lyrics search** — find a track by a line you remember

## Install

Download the latest build for your platform from the
[releases page](https://git.ljones.me/yonlu/yellowjacket/releases).

| Platform | Download |
|----------|----------|
| Linux | `yellowjacket-linux-amd64` |
| macOS | `yellowjacket-darwin-universal.app.zip` (Apple Silicon + Intel) |
| Windows | `yellowjacket-windows-amd64.exe` |

Prefer to build it yourself? See [Building from source](#building-from-source).

## Getting started

1. Launch YellowJacket.
2. Open **Settings** and add the folder(s) where your music lives.
3. Let the initial scan finish — you'll see progress as it works.
4. Browse by album, artist, or genre, queue something up, and press play.

Your library and settings are stored locally:

| | Linux / macOS | Windows |
|---|---|---|
| Config | `~/.config/yellowjacket/` | `%LOCALAPPDATA%\yellowjacket\config` |
| Library data | `~/.local/share/yellowjacket/` | `%LOCALAPPDATA%\yellowjacket\data` |

## Building from source

YellowJacket is built with [Go](https://go.dev/) and a
[Lit](https://lit.dev/)/TypeScript frontend, bridged by the
[Wails](https://wails.io/) framework.

**Prerequisites**

| Tool | Version |
|------|---------|
| Go | 1.25+ |
| Node.js | 22+ |
| pnpm | 10+ |
| Wails CLI | v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`) |

On Linux, install the system libraries Wails needs:

```bash
sudo apt-get install libasound2-dev libgtk-3-dev libwebkit2gtk-4.1-dev
```

macOS and Windows need no extra system packages. Run `wails doctor` to check your
environment.

**Build**

```bash
make setup        # install tooling and git hooks
make dev          # run with hot-reload
make build-prod   # produce a release binary
```

More detail for contributors lives in
[`docs/dev/overview.md`](./docs/dev/overview.md) and [`CLAUDE.md`](./CLAUDE.md).
