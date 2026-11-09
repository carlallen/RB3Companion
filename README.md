# RB3Companion

A self-hosted companion web app for [Rock Band 3 Deluxe](https://github.com/hmxmilohax/rock-band-3-deluxe) running [RB3Enhanced](https://github.com/RBEnhanced/RB3Enhanced). It listens for RB3Enhanced's network broadcast and turns it into:

- a live "now playing" dashboard (song, artist, album art, venue, band members' instruments/difficulty)
- a real-time view of the physical Stage Kit LEDs/strobe
- a searchable, clickable song library, synced from the console, that lets you jump straight to a song in-game
- an optional bridge that mirrors Stage Kit lighting cues to one or more [WLED](https://kno.wled.ge/) devices

## How it works

RB3Enhanced broadcasts game state as UDP packets (song info, score, band info, stage kit commands, etc.) per its [network protocol](https://github.com/RBEnhanced/RB3Enhanced). This server listens for that broadcast, keeps the latest state in memory, and pushes updates to connected browsers over a WebSocket.

When the game reaches the Music Library's song list screen, the server also fetches the full song list from RB3Enhanced's in-game HTTP server, persists it to a local SQLite database (so it survives restarts), and serves it to the dashboard for searching. Selecting a song there sends a "jump" command back to the console.

If WLED devices are configured, incoming Stage Kit commands are translated into WLED's WARLS realtime UDP protocol and sent to each enabled device, using a per-device channel-to-LED mapping you configure on the settings page.

## Running it

### Docker (recommended)

Prebuilt images are published to GHCR on every push to `main`: `ghcr.io/carlallen/rb3companion`.

```sh
docker run -d \
  --name rb3companion \
  -p 8080:8080 \
  -p 21070:21070/udp \
  -v rb3companion-store:/store \
  ghcr.io/carlallen/rb3companion:main
```

- Port `8080/tcp` serves the web dashboard and settings page.
- Port `21070/udp` must be reachable from the console/emulator running RB3Enhanced — configure RB3Enhanced's `network.json` to broadcast to this host's IP.
- The `/store` volume holds the SQLite database and any custom album art you drop in `/store/art`.

Or build the image yourself with the included `Dockerfile`:

```sh
docker build -t rb3companion .
```

### From source

Requires Go 1.18+. `go-sqlite3` uses cgo, so a C toolchain (gcc) must be available and `CGO_ENABLED=1`.

```sh
go build -o rb3companion ./cmd/server
./rb3companion
```

By default this serves on `:8080`, listens for RB3Enhanced on UDP `:21070`, and stores data under `./store`.

## Configuration

The server is configured entirely through environment variables:

| Variable    | Default                     | Description                                      |
|-------------|------------------------------|---------------------------------------------------|
| `ADDR`      | `:8080`                      | Address the HTTP server listens on                |
| `UDP_ADDR`  | `:21070`                     | Address the RB3Enhanced UDP listener binds to     |
| `STORE_DIR` | `store`                      | Directory for the database and custom album art   |
| `DB_PATH`   | `<STORE_DIR>/rb3companion.db` | Path to the SQLite database file                |

WLED devices, and each one's Stage Kit channel → LED mapping, are managed at runtime from the `/config` page — no restart required.

### Album art

When the song list loads, the app tries to download art for each new song from the [RB3art](https://github.com/carlallen/RB3art) album art database and saves it to `store/art`. Each song is only looked up once, and any failed downloads are retried the next time the song list loads.

To add other art (DLC, customs), drop a PNG named after the song's shortname in `store/art`, e.g. `store/art/<shortname>_keep.png`. Art already in `store/art` is never replaced by a download, and songs with no art show a blank placeholder.

## Development

```sh
go test ./...   # run tests
go run ./cmd/server
```

The frontend (`web/templates`, `web/static`) is plain HTML/CSS/JS served directly by the Go binary — no build step required.

## Project layout

```
cmd/server/        entrypoint: wiring, config, graceful shutdown
internal/rb3net/   UDP listener + protocol decoding, console HTTP client, WLED bridge, shared game-state hub
internal/server/   HTTP router, dashboard/config handlers, WebSocket endpoint
internal/db/       SQLite persistence for the song list and WLED device config
web/               HTML templates and static assets for the dashboard/config pages
store/             runtime data: SQLite DB and custom album art (git-ignored)
```

## Related projects

- [RB3Enhanced](https://github.com/RBEnhanced/RB3Enhanced) — the game-side mod this app talks to
- [RB3 Deluxe](https://github.com/hmxmilohax/rock-band-3-deluxe) — the game build this is designed for
- [ESP32-S2 Mini Stage Kit](https://github.com/Blasteroids/ESP32-S2-Mini-Stage-Kit) — a hardware alternative implementing the same network protocol
