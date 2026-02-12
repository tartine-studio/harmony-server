# Harmony Server

An open-source, self-hostable Discord alternative. Download a single binary, run it, done.

## Why Harmony?

Self-hosting a chat server shouldn't require a DevOps degree. Harmony is a single Go binary that gives you text chat, voice channels, and presence — no external database or configuration needed. Container images are also available if that's your preferred way to deploy.

```bash
./harmony-server
# That's it. SQLite database created automatically. Listening on :8080.
```

For larger communities, opt into PostgreSQL and Redis when you need them.

## Features

- **Text channels** with real-time messaging over WebSocket
- **Voice channels** with a built-in SFU (Pion WebRTC) — no STUN/TURN setup needed
- **Presence tracking** — online, idle, do not disturb
- **Zero-config by default** — embedded SQLite, in-memory pub/sub
- **Scales when you need it** — swap in PostgreSQL + Redis via environment variables

## Quick Start

```bash
# Run from source
go run cmd/harmony/main.go

# Or build and run
go build -o harmony-server cmd/harmony/main.go
./harmony-server
```

### Scaled Mode (optional)

```bash
docker compose up -d postgres redis

HARMONY_DB_URL=postgres://harmony:password@localhost:5432/harmony?sslmode=disable \
HARMONY_REDIS_URL=redis://localhost:6379/0 \
./harmony-server
```

## Architecture

Harmony is a clean monolith. Each domain module follows the **handler > service > repository** pattern, and storage backends are swappable through interfaces.

```
cmd/harmony/          # Entry point and dependency wiring
internal/
  auth/               # JWT authentication
  channel/            # Channel CRUD
  chat/               # Real-time messaging
  presence/           # Online/offline tracking
  voice/              # SFU (Pion WebRTC)
  ws/                 # WebSocket hub and connection management
  storage/            # Repository interfaces + backend implementations
    sqlite/           # Default — embedded, zero-config
    postgres/         # Opt-in — for larger deployments
    pubsub/           # In-memory (default) or Redis (opt-in)
```

## Tech Stack

- **Go** with `chi` router (thin `net/http` wrapper)
- **Gorilla WebSocket** for real-time communication
- **Pion WebRTC** for voice/video SFU
- **SQLite** (default) / **PostgreSQL** (opt-in)
- **Redis** (opt-in) for pub/sub and presence at scale

## Contributing

```bash
# Run tests
go test ./...

# Lint
golangci-lint run
```

The codebase is intentionally simple. No ORM, no magic — just interfaces, raw SQL, and straightforward Go. If you've been looking for a project where you can make a real impact without wading through layers of abstraction, this is it.

## License

AGPL-3.0 — see [LICENSE](LICENSE) for details.
