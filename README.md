# xdcc-go

A Go implementation of an XDCC downloader for IRC. Provides three command-line tools for searching, browsing, and downloading files from IRC bots via the XDCC protocol. Designed to run in a Docker container.

## Tools

| Command | Description |
|---|---|
| `xdcc-dl` | Download one or more packs given an XDCC message |
| `xdcc-search` | Search for packs and print ready-to-use download commands |
| `xdcc-browse` | Interactive search → filter → select → download |

---

## Installation

### Build from source

```sh
git clone https://github.com/bassista/xdcc
cd xdcc/xdcc-go
go build -o xdcc-dl     ./cmd/xdcc-dl
go build -o xdcc-search ./cmd/xdcc-search
go build -o xdcc-browse ./cmd/xdcc-browse
```

Requires Go 1.22+.

### Docker

The Dockerfile produces a minimal Alpine image with all three binaries.

```sh
docker build -t xdcc-go .

# Run a download inside the container
docker run --rm -v /my/downloads:/downloads xdcc-go \
  xdcc-dl "/msg MyBot xdcc send #42" -o /downloads

# Keep a persistent container and exec into it
docker run -d --name xdcc -v /my/downloads:/downloads xdcc-go
docker exec -it xdcc xdcc-browse "my show" -o /downloads
```

The image is built for `linux/arm64`. Change `GOARCH` in the Dockerfile to target a different architecture.

---

## xdcc-dl

Download one or more packs by passing the XDCC message string.

```
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

| Flag | Default | Description |
|---|---|---|
| `-s`, `--server` | *(auto)* | IRC server (`host` or `host:port`). Overrides automatic server detection from bot name |
| `-o`, `--out` | `.` | Output directory or file path |
| `-t`, `--throttle` | `-1` | Speed limit in bytes/s (e.g. `512K`, `2M`, `1G`). `-1` = unlimited |
| `--connect-timeout` | `120` | Seconds to wait for the bot to initiate the DCC transfer |
| `--stall-timeout` | `60` | Seconds of no transfer progress before aborting. `0` = disabled |
| `--fallback-channel` | *(none)* | IRC channel to join if WHOIS returns no channels for the bot |
| `--wait-time` | `0` | Extra seconds to wait before sending the XDCC request |
| `--username` | *(random)* | IRC nickname (a random suffix is always appended) |
| `--channel-join-delay` | `-1` | Seconds to wait after connecting before sending WHOIS. `-1` = random 5–10 s |
| `-v`, `--verbose` | | Increase verbosity (repeatable: `-v`, `-vv`) |
| `-q`, `--quiet` | | Suppress all output including progress |

### Verbosity levels

| Flag | Shows |
|---|---|
| *(default)* | Connecting, download progress, final result |
| `-v` | + bot notices, channel joins, WHOIS results |
| `-vv` | + DNS resolution, DCC details, all IRC events |
| `-q` | nothing |

### Examples

```sh
# Download a single pack
xdcc-dl "/msg WoNd|SERIE-TV|04 xdcc send #2407"

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

```
xdcc-search <search_term> [engine] [flags]
```

The engine can be passed as a second positional argument or via `--search-engine`. Default is `xdcc-eu`.

Available engines: `xdcc-eu`, `nibl`, `ixirc`, `subsplease`

### Flags

| Flag | Default | Description |
|---|---|---|
| `--search-engine` | `xdcc-eu` | Search engine to use |
| `-v`, `--verbose` | | Show search engine debug info |

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

# Specify engine as flag
xdcc-search "my show" --search-engine=ixirc

# Verbose (shows HTTP requests and parsing details)
xdcc-search "my show" -v

# Pipe into grep
xdcc-search "my show" | grep -i "s01e03"
```

---

## xdcc-browse

Interactive search → filter → numbered list → selection → download.

```
xdcc-browse <search_term> [flags]
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `--search-engine` | `xdcc-eu` | Search engine to use: `nibl`, `xdcc-eu`, `ixirc`, `subsplease` |
| `--ext` | *(none)* | Filter results by file extension(s), comma-separated (e.g. `mkv,avi,mp4`) |
| `--bot` | *(none)* | Filter results by bot name substring, case-insensitive (e.g. `WOND`) |
| `-s`, `--server` | *(from search)* | Override IRC server for all selected packs (`host` or `host:port`) |
| `-o`, `--out` | `.` | Output directory or file path |
| `-t`, `--throttle` | `-1` | Speed limit in bytes/s (e.g. `512K`, `2M`, `1G`). `-1` = unlimited |
| `--connect-timeout` | `120` | Seconds to wait for the bot to initiate the DCC transfer |
| `--stall-timeout` | `60` | Seconds of no transfer progress before aborting. `0` = disabled |
| `--fallback-channel` | *(none)* | IRC channel to join if WHOIS returns no channels for the bot |
| `--wait-time` | `0` | Extra seconds to wait before sending the XDCC request |
| `--username` | *(random)* | IRC nickname (a random suffix is always appended) |
| `--channel-join-delay` | `-1` | Seconds to wait after connecting before sending WHOIS. `-1` = random 5–10 s |
| `-v`, `--verbose` | | Increase verbosity (repeatable: `-v`, `-vv`) |
| `-q`, `--quiet` | | Suppress all output including progress |

### Selection syntax

After the numbered list is shown you will be prompted for a selection:

| Input | Meaning |
|---|---|
| `3` | single pack |
| `1-5` | range (packs 1 through 5) |
| `1,3,7` | comma-separated list |
| `all` | download everything in the list |

### Examples

```sh
# Basic interactive search
xdcc-browse "my show"

# Filter to MKV files only from bots containing "WOND"
xdcc-browse "my show" --ext=mkv --bot=WOND

# Use a different engine and save to a specific directory
xdcc-browse "my show" --search-engine=nibl -o /downloads

# Verbose download after selection
xdcc-browse "my show" -v

# Filter and override server
xdcc-browse "my show" --ext=mkv --server=94.23.150.97
```

---

## Notes

### Automatic server detection

`xdcc-dl` and `xdcc-browse` attempt to detect the correct IRC server from the bot name prefix (e.g. `TLT*` → `irc.williamgattone.it`). Use `--server` to override when automatic detection fails or when your DNS provider blocks the hostname.

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
| Server unreachable (DNS block) | Abort, suggest `--server` |
| File already downloaded | Skip |
