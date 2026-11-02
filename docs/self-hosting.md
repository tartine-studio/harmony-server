# Self-hosting

## Run it

```bash
./harmony-server
```

On first start, the server:

- creates the data directory (`./data` by default);
- creates the SQLite database `data/harmony.db` and runs the migrations (also on every later start, so upgrading is just replacing the binary);
- generates a random JWT signing secret and saves it to `data/.jwt_secret` (unless `HARMONY_JWT_SECRET` is set);
- listens for HTTP/WebSocket on `0.0.0.0:8080` and for voice on UDP `50000`.

There is nothing else to install: no external database or cache.

## Configuration

Everything is set through environment variables, and every variable is optional.

| Variable | Default | Description |
|---|---|---|
| `HARMONY_HOST` | `0.0.0.0` | HTTP listen address |
| `HARMONY_PORT` | `8080` | HTTP listen port (REST + WebSocket) |
| `HARMONY_DATA_DIR` | `./data` | Where the database and the JWT secret live |
| `HARMONY_JWT_SECRET` | *(generated)* | Token signing secret. If unset, read from / generated into `DATA_DIR/.jwt_secret`. Changing it logs everyone out. |
| `HARMONY_JWT_ACCESS_TTL` | `15m` | Access token lifetime |
| `HARMONY_JWT_REFRESH_TTL` | `168h` | Refresh token lifetime (7 days) |
| `HARMONY_PRESENCE_IDLE_TIMEOUT` | `5m` | Inactivity before a user shows as idle |
| `HARMONY_PRESENCE_OFFLINE_GRACE` | `10s` | Delay before announcing offline after the last disconnect |
| `HARMONY_VOICE_UDP_PORT` | `50000` | The single UDP port used by all voice traffic |
| `HARMONY_PUBLIC_IP` | *(unset)* | Address announced to voice clients in addition to the local ones (see below) |
| `HARMONY_VOICE_MAX_PARTICIPANTS` | `25` | Maximum users per voice channel |

Durations use Go syntax: `30s`, `5m`, `1h30m`.

> PostgreSQL and Redis ("scaled mode", `HARMONY_DB_URL`, `HARMONY_REDIS_URL`) are planned but **not implemented yet**: Harmony currently always uses the embedded SQLite database.

## Ports

| Port | Protocol | Used for |
|---|---|---|
| `8080` (`HARMONY_PORT`) | TCP | REST API and WebSocket |
| `50000` (`HARMONY_VOICE_UDP_PORT`) | **UDP** | Voice (WebRTC) |

Open both in your firewall. Voice won't work if only the TCP port is reachable.

## Voice networking

The server tells voice clients which IP addresses to send audio to (its ICE candidates). By default these are the addresses of the machine's own network interfaces, which is fine on a LAN or on a server with a public IP.

Set `HARMONY_PUBLIC_IP` when clients can't reach those addresses directly:

- **Behind a home router / NAT**: set it to your public IP, and forward UDP `50000` to the server.
- **In Docker** (bridge network): set it to the host's IP, and publish the port as UDP: `-p 50000:50000/udp`.

The public IP is *added* to the local addresses, so LAN clients keep working. Harmony never contacts third-party services (no STUN lookup) to discover it.

## TLS

Harmony serves plain HTTP. Put it behind a reverse proxy (Caddy, nginx, Traefik…) for HTTPS. The proxy must:

- forward WebSocket upgrades on `/api/ws`;
- not time out idle WebSockets in less than ~60 s (the server pings every 54 s).

Voice (UDP) does not go through the proxy: it is encrypted end-to-end between client and server with DTLS-SRTP.

Example Caddyfile:

```
chat.example.com {
    reverse_proxy localhost:8080
}
```

## Data and backups

Everything persistent is in `HARMONY_DATA_DIR`:

| File | Content |
|---|---|
| `harmony.db` (+ `-wal`, `-shm`) | SQLite database: users (bcrypt password hashes), channels, messages |
| `.jwt_secret` | Token signing secret. Keep it private. |

Presence and voice state are in memory only and are not backed up.

To back up a running server, use SQLite's online backup (copying only `harmony.db` while the server runs can miss recent writes that are still in the `-wal` file):

```bash
sqlite3 data/harmony.db ".backup 'harmony-backup.db'"
```

Or stop the server and copy the whole data directory.

## Security notes

- There is **no role or permission system yet**: any registered user can create/rename/delete channels and edit/delete other users. Only run an instance for people you trust, and don't expose registration to the internet if that matters to you.
- Anyone who can reach the server can register an account.
- The WebSocket endpoint doesn't check the `Origin` header yet.
