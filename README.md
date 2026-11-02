# Harmony Server

**Harmony** is an open-source, self-hostable alternative to Discord: text channels, voice channels and presence for your community, on your own machine.

This repository is the **server**: a single Go binary with no external dependencies. Download it, run it, and your community has a home.

```bash
./harmony-server
# Database created, listening on :8080 (HTTP) and :50000/udp (voice). Done.
```

## Why Harmony?

Self-hosting a chat server shouldn't require a DevOps degree. Harmony is built around one idea: **deployment must be trivial**.

- **One binary, zero dependencies.** The SQLite database is embedded and created on first run; migrations run automatically.
- **Zero configuration.** Sensible defaults for everything; a handful of environment variables when you need them.
- **One instance = one community.** No multi-server/guild abstraction: you run Harmony for your group, and that's it.
- **Your data stays home.** The server never calls third-party services.

## Features

- **Text channels**: real-time messaging over WebSocket, with persistent history, editing and deletion by authors.
- **Voice channels**: a built-in SFU (based on [Pion](https://github.com/pion/webrtc)) relays Opus audio. No STUN/TURN server to run, and everything goes through a single UDP port. Mute and deafen are enforced server-side, and speaking indicators are detected by the server.
- **Presence**: online / idle / offline, derived by the server from client activity, plus a persistent "invisible" mode.
- **Multi-device**: stay connected from several devices; voice follows the device you join from.
- **Documented protocol** for writing your own client (see below).

## Quick start

### From source

Requires Go 1.25+ and a C compiler (for SQLite).

```bash
go build -o harmony-server ./cmd
./harmony-server
```

### Ports

| Port | Protocol | Purpose |
|---|---|---|
| `8080` | TCP | REST API + WebSocket |
| `50000` | **UDP** | Voice |

Behind NAT or in Docker, set `HARMONY_PUBLIC_IP` so voice clients can reach the server.

### Configuration

Everything is optional:

```bash
HARMONY_PORT=8080                 # HTTP port
HARMONY_DATA_DIR=./data           # database + generated JWT secret
HARMONY_VOICE_UDP_PORT=50000      # single UDP port for all voice traffic
HARMONY_PUBLIC_IP=203.0.113.10    # announce this IP to voice clients (NAT/Docker)
HARMONY_VOICE_MAX_PARTICIPANTS=25 # per voice channel
```

See **[docs/self-hosting.md](docs/self-hosting.md)** for every option, TLS with a reverse proxy, and backups.

## Building a client

Harmony's protocol is documented so anyone can write a compatible client:

| | |
|---|---|
| [docs/README.md](docs/README.md) | Overview and client lifecycle: start here |
| [docs/rest-api.md](docs/rest-api.md) | Auth, users, channels, message history |
| [docs/websocket.md](docs/websocket.md) | Real-time protocol and chat events |
| [docs/presence.md](docs/presence.md) | Presence model and events |
| [docs/voice.md](docs/voice.md) | Voice signaling and WebRTC requirements |

The official client (Tauri/Rust) lives in a separate repository.

## Project status

Harmony is **pre-1.0** and under active development. The protocol may still change.

Working today: accounts and JWT auth, text channels, real-time chat, presence, voice channels.

Not there yet:

- Roles and permissions: **every user can currently manage channels and users**. Only invite people you trust.
- Message pagination, per-channel event scoping, real-time channel/user updates
- PostgreSQL + Redis "scaled mode" for large communities
- Container image, file uploads, video / screen sharing

## Architecture

Harmony is a clean monolith following a **hexagonal architecture** (ports and adapters): the domain has no dependency on frameworks or infrastructure, and adapters plug in from the outside.

```
cmd/
  main.go                 # Entry point: load config, start the server
  voicebot/               # Headless voice client for testing the SFU
internal/
  domain/                 # Entities and ports (interfaces)
  application/            # Use cases: auth, users, channels, messages, presence, voice
  adapter/
    http/                 # REST handlers, router, auth middleware
    ws/                   # WebSocket hub, client sessions, event dispatch
    sfu/                  # Pion SFU (voice media plane)
    repository/           # SQLite repositories
    token/                # JWT
  server/                 # Composition root (wires everything) + end-to-end tests
  voiceclient/            # Reference Go voice client (used by voicebot and tests)
  config/                 # Environment configuration
db/migrations/            # SQL migrations (goose), embedded in the binary
docs/                     # Protocol and self-hosting documentation
```

Built with Go, [chi](https://github.com/go-chi/chi), [gorilla/websocket](https://github.com/gorilla/websocket), [Pion WebRTC](https://github.com/pion/webrtc), SQLite, [goose](https://github.com/pressly/goose) and [zap](https://github.com/uber-go/zap).

## Contributing

```bash
go run ./cmd                      # run the server (zero config)
go test -race ./...               # all tests: unit, SQLite integration, end-to-end (incl. real WebRTC audio)
go test -short ./...              # skip the slower WebRTC media test
golangci-lint run                 # lint

# Voice without a client: two headless bots
go run ./cmd/voicebot -user alice -register -channel lounge -create -speak
go run ./cmd/voicebot -user bob -register -channel lounge
```

Tests live next to the code they cover:

- `internal/application`: service logic with in-memory fakes (presence timers use `testing/synctest`)
- `internal/adapter/repository`: SQLite repositories against a real database
- `internal/server`: the real server over HTTP and WebSocket, including audio forwarding, mute and deafen with real WebRTC peers

The codebase is intentionally simple: no ORM, no magic, just interfaces, raw SQL and straightforward Go. Issues and pull requests are welcome.

## License

[AGPL-3.0](LICENSE)
