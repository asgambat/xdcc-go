# xdcc-go

A Go implementation of an XDCC downloader for IRC. Provides a **daemon server** with REST API + web UI, plus three command-line tools for searching, browsing, and downloading files from IRC bots via the XDCC protocol.

All four binaries are pure Go (zero CGO), cross-compile natively for `linux/amd64` and `linux/arm64` (Raspberry Pi), and fit in a single Docker image.

## Tools

| Command | Description |
|---|---|
| `xdcc-server` | **Daemon** — persistent IRC connections, download queue, REST API, web UI (Svelte) |
| `xdcc-dl` | Download one or more packs given an XDCC message |
| `xdcc-search` | Search for packs and print ready-to-use download commands |
| `xdcc-browse` | Interactive search → filter → select → download |

The CLI tools (`xdcc-dl`, `xdcc-browse`) can delegate operations to a running `xdcc-server` via the `--command-server` flag, enabling remote control without direct IRC access.

---

## Quick Start — Server Mode (Recommended)

```sh
# Start the server
xdcc-server

# Open http://localhost:8080 in your browser (web UI)
```

The server connects to default IRC servers, manages a persistent download queue, and exposes a REST API + real-time SSE event stream.

### Docker

```sh
# Build the image (includes all binaries + web UI)
docker build -t xdcc-go .

# Run the server with persistent data volume
docker run -d \
  --name xdcc \
  -p 8080:8080 \
  -v xdcc-data:/data \
  xdcc-go

# Open http://localhost:8080
```

The `/data` volume persists:
- **SQLite database** (download queue, server config, watchlists, presets)
- **Completed downloads** (`/data/downloads/complete/`)
- **Partial downloads** (`/data/downloads/tmp/`)
- **Logs** (`/data/logs/xdcc-server.log`)

### Docker Compose

```yaml
# docker-compose.yml
services:
  xdcc-server:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - xdcc-data:/data
    restart: unless-stopped

volumes:
  xdcc-data:
```

### CLI Commands (Standalone)

The classic CLI tools work independently of the server — no daemon required:

```sh
# Download a pack
xdcc-dl "/msg MyBot xdcc send #42"

# Search for packs
xdcc-search "my show"

# Interactive browse + download
xdcc-browse "my show"
```

### CLI Delegation (Client-Server Mode)

All CLI tools can delegate to a running `xdcc-server`:

```sh
# Delegate download to server (server manages IRC connection + queue)
xdcc-dl "/msg MyBot xdcc send #42" --command-server=http://localhost:8080 -o /downloads

# Delegate search + download to server
xdcc-browse "my show" --command-server=http://localhost:8080 --ext=mkv -o /downloads
```

When using `--command-server`:
- `xdcc-dl` delegates the download and polls progress (speed, ETA, %) every second
- `xdcc-browse` performs the search via the server, shows the interactive selection menu, then delegates selected packs
- If the server is unreachable, the command fails with a clear error — no silent fallback to standalone mode
- Server version compatibility is checked before any delegation

---

## Installation

### Build from source

```sh
git clone https://github.com/asgambat/xdcc-go
cd xdcc-go

# Build individual binaries
go build -o xdcc-dl      ./cmd/xdcc-dl
go build -o xdcc-server  ./cmd/xdcc-server
go build -o xdcc-search  ./cmd/xdcc-search
go build -o xdcc-browse  ./cmd/xdcc-browse

# Or build all at once
go build ./cmd/...
```

Requires Go 1.22+.

### Build the full stack (frontend + backend)

For the server with embedded web UI, build the frontend first:

```sh
cd web
npm install && npm run build
cd ..
go build -o xdcc-server ./cmd/xdcc-server
```

### Docker

```sh
# Single architecture
docker build -t xdcc-go .

# Multi-architecture (AMD64 + ARM64)
docker buildx build --platform=linux/amd64,linux/arm64 -t xdcc-go . --push
```

---

## xdcc-server

Persistent daemon that manages IRC connections and downloads. Exposes a REST API and serves a web UI.

```sh
xdcc-server [flags]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--config` | `config.yaml` | Path to configuration file |
| `--port` | *(from config)* | HTTP server port (overrides config) |
| `--download-dir` | *(from config)* | Destination directory for completed downloads |
| `--temp-dir` | *(from config)* | Temporary directory for in-progress downloads |

Configuration priority: **CLI flags > environment variables > config.yaml**.

### Web UI

The server serves a responsive web UI (Svelte 5 PWA) at `http://localhost:8080`:

| Page | Description |
|---|---|
| **Dashboard** | Overview: active downloads, disk space, connected IRC servers |
| **Servers** | Manage IRC server connections and channels |
| **Downloads** | View queue, reorder, pause/resume/retry/remove |
| **Search** | Aggregated search across all providers (xdcc-eu, nibl, subsplease) |
| **Presets** | Save and reuse search queries with filters |
| **Watchlists** | Monitor for new results automatically |
| **Providers** | Provider health, latency stats, enable/disable at runtime |
| **Settings** | Server configuration |

### Systemd (Raspberry Pi / Linux)

An example systemd unit file is provided in `examples/xdcc-server.service`:

```sh
# Create the xdcc user
sudo adduser --system --no-create-home xdcc

# Create data directories
sudo mkdir -p /var/lib/xdcc-server/downloads/{tmp,complete}
sudo mkdir -p /var/log/xdcc-server
sudo chown -R xdcc:xdcc /var/lib/xdcc-server /var/log/xdcc-server

# Copy the binary and config
sudo cp xdcc-server /usr/local/bin/
sudo mkdir -p /etc/xdcc-server
sudo cp config.yaml /etc/xdcc-server/

# Install and enable the service
sudo cp examples/xdcc-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now xdcc-server

# Check status
sudo systemctl status xdcc-server

# View logs
sudo journalctl -u xdcc-server -f
```

The server auto-starts on boot (`WantedBy=multi-user.target`) with `Restart=on-failure` and a 10-second delay between restarts.

---

## xdcc-dl

Download one or more packs by passing the XDCC message string.

```sh
xdcc-dl <message> [flags]
```

### Message format

```
/msg <bot> xdcc send #<pack>
```

Pack number supports ranges, steps, and lists:

| Syntax | Meaning |
|---|---|
| `#5` | single pack |
| `#1-10` | packs 1 through 10 |
| `#1-10;2` | packs 1, 3, 5, 7, 9 (every 2nd) |
| `#1,3,7` | specific packs |

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--command-server` | | *(none)* | Delegate download to a remote xdcc-server (e.g. `http://localhost:8080`) |
| `--server` | `-s` | *(auto)* | IRC server (`host` or `host:port`). Overrides automatic server detection from bot name |
| `--out` | `-o` | `.` | Output directory or file path |
| `--throttle` | `-t` | `-1` | Speed limit in bytes/s (e.g. `512K`, `2M`, `1G`). `-1` = unlimited |
| `--connect-timeout` | `-C` | `120` | Seconds to wait for the bot to initiate the DCC transfer |
| `--stall-timeout` | `-S` | `60` | Seconds of no transfer progress before aborting. `0` = disabled |
| `--fallback-channel` | `-f` | *(none)* | IRC channel to join if WHOIS returns no channels for the bot |
| `--wait-time` | `-w` | `0` | Extra seconds to wait before sending the XDCC request |
| `--username` | `-u` | *(random)* | IRC nickname (a random suffix is always appended) |
| `--channel-join-delay` | `-D` | `-1` | Seconds to wait after connecting before sending WHOIS. `-1` = random 5–10 s |
| `--dns-server` | `-d` | `8.8.8.8:53` | Fallback DNS resolver when system DNS is blocked (`host:port`) |
| `--verbose` | `-v` | | Increase verbosity (repeatable: `-v`, `-vv`) |
| `--quiet` | `-q` | | Reduce output (repeatable: `-q`, `-qq`) |

### Verbosity levels

| Flag | Shows |
|---|---|
| *(default)* | Connecting, download progress, final result |
| `-v` | + bot notices, channel joins, WHOIS results |
| `-vv` | + DNS resolution, DCC details, all IRC events |
| `-q` | Hides connection info; keeps errors, bot notices, and progress |
| `-qq` | Suppresses all output |

> If `-q` and `-v` are used together, `-q` takes precedence and `-v` is ignored.

### Examples

```sh
# Download a single pack (standalone)
xdcc-dl "/msg WoNd|SERIE-TV|04 xdcc send #2407"

# Delegate download to a running server (server manages IRC)
xdcc-dl "/msg WoNd|SERIE-TV|04 xdcc send #2407" --command-server=http://localhost:8080

# Download with verbose output and custom output directory
xdcc-dl "/msg WoNd|SERIE-TV|04 xdcc send #2407" -v -o /tmp/downloads

# Download a range of packs with speed cap
xdcc-dl "/msg MyBot xdcc send #1-10" --throttle=2M

# Override server (useful if DNS is blocked on your network)
xdcc-dl "/msg WoNd|SERIE-TV|04 xdcc send #2407" --server=94.23.150.97:6667

# Full debug output
xdcc-dl "/msg MyBot xdcc send #5" -vv
```

---

## xdcc-search

Search for packs and print one result per line with the corresponding `xdcc-dl` command.

```sh
xdcc-search <search_term> [engine] [flags]
```

The engine can be passed as a second positional argument or via `--search-engine`. Default is `xdcc-eu`.

Available engines: `xdcc-eu`, `nibl`, `subsplease`

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--search-engine` | `-e` | `xdcc-eu` | Search engine to use |
| `--compact` | `-c` | `false` | Remove duplicate results with same filename, size and bot family |
| `--prefix` | `-p` | `false` | Keep only results whose filename starts with the search term (case-insensitive) |
| `--verbose` | `-v` | | Show search engine debug info |

### Output format

```
<filename> [<size>] (xdcc-dl "<message>" [--server <host>])
```

### Examples

```sh
# Search using the default engine (xdcc-eu)
xdcc-search "my show"

# Specify engine as positional argument
xdcc-search "my show" nibl

# Only results whose filename starts with the search term
xdcc-search "my show" --prefix

# Verbose (shows HTTP requests and parsing details)
xdcc-search "my show" -v

# Pipe into grep
xdcc-search "my show" | grep -i "s01e03"
```

---

## xdcc-browse

Interactive search → filter → numbered list → selection → download.

```sh
xdcc-browse <search_term> [flags]
```

### Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--command-server` | | *(none)* | Delegate search and download to a remote xdcc-server (e.g. `http://localhost:8080`) |
| `--search-engine` | `-e` | `xdcc-eu` | Search engine to use: `nibl`, `xdcc-eu`, `subsplease` |
| `--ext` | `-x` | *(none)* | Filter results by file extension(s), comma-separated (e.g. `mkv,avi,mp4`) |
| `--bot` | `-b` | *(none)* | Filter results by bot name substring, case-insensitive (e.g. `WOND`) |
| `--prefix` | `-p` | `false` | Keep only results whose filename starts with the search term (case-insensitive) |
| `--server` | `-s` | *(from search)* | Override IRC server for all selected packs (`host` or `host:port`) |
| `--out` | `-o` | `.` | Output directory or file path |
| `--throttle` | `-t` | `-1` | Speed limit in bytes/s (e.g. `512K`, `2M`, `1G`). `-1` = unlimited |
| `--connect-timeout` | `-C` | `120` | Seconds to wait for the bot to initiate the DCC transfer |
| `--stall-timeout` | `-S` | `60` | Seconds of no transfer progress before aborting. `0` = disabled |
| `--fallback-channel` | `-f` | *(none)* | IRC channel to join if WHOIS returns no channels for the bot |
| `--wait-time` | `-w` | `0` | Extra seconds to wait before sending the XDCC request |
| `--username` | `-u` | *(random)* | IRC nickname (a random suffix is always appended) |
| `--channel-join-delay` | `-D` | `-1` | Seconds to wait after connecting before sending WHOIS. `-1` = random 5–10 s |
| `--dns-server` | `-d` | `8.8.8.8:53` | Fallback DNS resolver when system DNS is blocked (`host:port`) |
| `--compact` | `-c` | `false` | Remove duplicate results with same filename, size and bot family |
| `--verbose` | `-v` | | Increase verbosity (repeatable: `-v`, `-vv`) |
| `--quiet` | `-q` | | Reduce output (repeatable: `-q`, `-qq`) |

> If `-q` and `-v` are used together, `-q` takes precedence and `-v` is ignored.

### Selection syntax

After the numbered list is shown you will be prompted for a selection:

| Input | Meaning |
|---|---|
| `3` | single pack |
| `1-5` | range (packs 1 through 5) |
| `1+5` | count (5 consecutive packs starting from 1, i.e. packs 1–5) |
| `1,3,7` | comma-separated list |
| `all` | download everything in the list |

### Examples

```sh
# Basic interactive search
xdcc-browse "my show"

# Delegate search + download to a running server
xdcc-browse "my show" --command-server=http://localhost:8080

# Filter to MKV files only from bots containing "WOND"
xdcc-browse "my show" --ext=mkv --bot=WOND

# Only results whose filename starts with the search term
xdcc-browse "my show" --prefix

# Use a different engine and save to a specific directory
xdcc-browse "my show" --search-engine=nibl -o /downloads

# Verbose download after selection
xdcc-browse "my show" -v

# Filter and override server
xdcc-browse "my show" --ext=mkv --server=94.23.150.97
```

---

## Notes

### Server Configuration

Configuration is loaded from `config.yaml`. See the file for all available settings. Key environment variables:

| Variable | Default | Description |
|---|---|---|
| `XDCC_HTTP_PORT` | `8080` | HTTP server port |
| `XDCC_IRC_NICKNAME` | `xdcc-user` | Base IRC nickname |
| `XDCC_DOWNLOAD_TEMP_DIR` | `./downloads/tmp` | Temp directory for partial downloads |
| `XDCC_DOWNLOAD_DEST_DIR` | `./downloads/complete` | Destination for completed downloads |
| `XDCC_DOWNLOAD_MAX_PARALLEL` | `5` | Global max parallel downloads |
| `XDCC_DOWNLOAD_MIN_DISK_SPACE` | `1GB` | Minimum free disk space before pausing queue |
| `XDCC_DOWNLOAD_MAX_RETRY` | `3` | Max retry attempts per download |
| `XDCC_LOGGING_LEVEL` | `info` | Log level: debug, info, warn, error |
| `XDCC_LOGGING_FILE_PATH` | *(stderr)* | Log file path |
| `XDCC_SEARCH_CACHE_ENABLED` | `true` | Enable search result caching |

### DNS fallback

When the system DNS returns a blocked address (`0.0.0.0` / `::`) or fails entirely, the client automatically retries the lookup via a public DNS resolver (default: `8.8.8.8:53`). Use `--dns-server` to specify a different resolver (e.g. `1.1.1.1:53`). The resolved IP is passed directly to the IRC library so no further blocked lookups occur during the connection.

### Multi-IP connection failover

The hostname is resolved to **all** available IPs (system DNS + fallback DNS combined). If the connection to the first IP fails or times out, the client automatically tries the next IP in the list until one succeeds or all have been exhausted. Progress is reported in the log (e.g. `IP 2/3: …`). This makes connections resilient to partially unreachable servers or round-robin DNS entries where some addresses are down.

### Automatic server detection

`xdcc-dl` and `xdcc-browse` attempt to detect the correct IRC server from the bot name prefix (e.g. `TLT*` → `irc.williamgattone.it`, `WeC*` → `irc.explosionirc.net`). For all other bots the default server `irc.rizon.net` is used. Use `--server` to override when automatic detection fails or when your DNS provider blocks the hostname.

### File resume

If a partial file already exists at the destination, the download is automatically resumed from where it left off using the DCC RESUME/ACCEPT protocol.

### Stall detection

Once the transfer starts, a stall watchdog checks for progress every few seconds. If no bytes are received for `--stall-timeout` seconds the download is aborted and retried (up to 3 times).

### Retry behaviour

| Error | Behaviour |
|---|---|
| Timeout / stall | Retry up to 3 times |
| Pack already requested | Wait 60 s, then retry |
| Bot denied / slot busy | Abort, show bot message |
| Bot not found | Abort |
| Server unreachable | Try all resolved IPs, then abort and suggest `--server` |
| File already downloaded | Skip |

### Compatibility

All four binaries (xdcc-server, xdcc-dl, xdcc-search, xdcc-browse) are **pure Go** with **zero CGO** dependencies. They compile natively for `linux/amd64`, `linux/arm64` (Raspberry Pi 4/5), `darwin/amd64`, and `darwin/arm64` (Apple Silicon). The Dockerfile uses multi-stage builds with `CGO_ENABLED=0` for portable cross-compilation.
