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
go run cmd/main.go

# Or build and run
go build -o harmony-server cmd/main.go
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

Harmony follows a **hexagonal architecture** (ports and adapters). Domain logic has zero dependencies on frameworks or infrastructure — adapters plug in from the outside.

```
cmd/                              # Entry point, dependency wiring
internal/
  domain/                         # Core types, interfaces (ports)
  application/                    # Use cases / business logic (services)
  adapter/
    http/                         # REST handlers, router, middleware
    repository/                   # SQLite implementation (default)
    token/                        # JWT implementation
  config/                         # Environment-based configuration
migrations/                       # SQL migration files (goose)
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
