# REST API

Base path: `/api`. Requests and responses are JSON (`Content-Type: application/json`).

## Errors

Every error response has the same shape:

```json
{ "error": "human readable message", "code": "ERROR_CODE" }
```

Use `code` in your logic; `error` is for logs and may change.

| HTTP | `code` | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Malformed JSON or a field failing validation (message names the field) |
| 400 | `REGISTRATION_FAILED` | Registration refused (e.g. email already used: deliberately vague, to prevent account enumeration) |
| 400 | `UPDATE_FAILED` | Profile update refused (e.g. the new email is already used by another account) |
| 401 | `INVALID_CREDENTIALS` | Wrong email or password |
| 401 | `INVALID_TOKEN` | Refresh token invalid, expired, or not a refresh token |
| 401 | `UNAUTHORIZED` | Missing/invalid/expired access token on a protected route |
| 403 | `FORBIDDEN` | Editing or deleting someone else's message |
| 404 | `NOT_FOUND` | Unknown user, channel or message |
| 500 | `INTERNAL_ERROR` | Server-side failure (details are only in the server logs) |

## Authentication

Harmony uses JWTs (HS256). Login returns a pair:

| Token | Default lifetime | Used for |
|---|---|---|
| `accessToken` | 15 minutes | `Authorization: Bearer …` on REST, and the WebSocket handshake |
| `refreshToken` | 7 days | Only `POST /api/auth/refresh` |

Each token is only accepted where it belongs: an access token is rejected by `/auth/refresh`, and a refresh token is rejected as a bearer token. Treat tokens as opaque. There is no logout endpoint and no server-side revocation: to log out, delete the tokens client-side. Refreshing does not invalidate the previous refresh token.

Lifetimes are configurable by the server admin (`HARMONY_JWT_ACCESS_TTL`, `HARMONY_JWT_REFRESH_TTL`): don't hard-code them. Refresh when a request returns `401 UNAUTHORIZED`, or proactively before expiry.

### `POST /api/auth/register`

Public.

```json
{ "username": "alice", "email": "alice@example.com", "password": "correct horse" }
```

| Field | Rules |
|---|---|
| `username` | required, 2–32 characters. Not unique. |
| `email` | required, valid email, unique |
| `password` | required, 8–128 characters |

**201 Created**: the new user. Registration does **not** log in; call `/auth/login` next.

```json
{
  "id": "0b8e…",
  "username": "alice",
  "email": "alice@example.com",
  "invisible": false,
  "createdAt": "2026-11-02T16:04:05.123456Z",
  "updatedAt": "2026-11-02T16:04:05.123456Z"
}
```

> This response's timestamps carry fractional seconds, unlike every other endpoint. Parse timestamps with a full RFC 3339 parser.

Errors: `400 VALIDATION_ERROR`, `400 REGISTRATION_FAILED`.

### `POST /api/auth/login`

Public.

```json
{ "email": "alice@example.com", "password": "correct horse" }
```

**200 OK**

```json
{ "accessToken": "eyJ…", "refreshToken": "eyJ…" }
```

Errors: `400 VALIDATION_ERROR`, `401 INVALID_CREDENTIALS` (same error for unknown email and wrong password).

### `POST /api/auth/refresh`

Public.

```json
{ "refreshToken": "eyJ…" }
```

**200 OK**: a brand new token pair (same shape as login). Errors: `400 VALIDATION_ERROR`, `401 INVALID_TOKEN` (expired, malformed, an access token, or the user was deleted).

## Users

All routes below require `Authorization: Bearer <accessToken>`.

User object:

```json
{
  "id": "0b8e…",
  "username": "alice",
  "email": "alice@example.com",
  "createdAt": "2026-11-02T16:04:05Z",
  "updatedAt": "2026-11-02T16:04:05Z"
}
```

The invisible flag is not part of this object; your own value comes with the presence snapshot (see [presence.md](presence.md)).

| Route | Description | Success |
|---|---|---|
| `GET /api/users/me` | Current user | 200, user |
| `PATCH /api/users/me` | Update own profile | 200, user / 400 |
| `GET /api/users` | All users (order not guaranteed) | 200, array of users |
| `GET /api/users/{id}` | One user | 200, user / 404 |
| `PATCH /api/users/{id}` | Update any user (no roles yet) | 200, user / 400 / 404 |
| `DELETE /api/users/{id}` | Delete any user (no roles yet) | 204 / 404 |

Update body: every field is optional, and a missing or empty field is left unchanged.

```json
{ "username": "Alice", "email": "alice@new.example" }
```

| Field | Rules |
|---|---|
| `username` | 2–32 characters |
| `email` | valid email, not used by another account (otherwise `400 UPDATE_FAILED`) |

Deleting a user keeps their messages: their `userId` becomes `null` (see [Messages](#messages)).

## Channels

Channel object:

```json
{
  "id": "5f1c…",
  "name": "general",
  "type": "text",
  "createdAt": "2026-11-02T16:04:05Z",
  "updatedAt": "2026-11-02T16:04:05Z"
}
```

`type` is `"text"` or `"voice"` and cannot be changed after creation.

| Route | Description | Success |
|---|---|---|
| `GET /api/channels` | All channels (order not guaranteed: sort client-side) | 200, array |
| `POST /api/channels` | Create a channel | 201, channel |
| `GET /api/channels/{id}` | One channel | 200 / 404 |
| `PATCH /api/channels/{id}` | Rename | 200 / 404 |
| `DELETE /api/channels/{id}` | Delete: its messages are deleted, and everyone in it (voice) is disconnected | 204 / 404 |

Create body: `{ "name": "general", "type": "text" }`. `name` is 1–100 characters and `type` is one of `text`, `voice`.
Rename body: `{ "name": "lobby" }`.

Channel changes are not broadcast over the WebSocket: re-fetch the list when you need it.

## Messages

Message object (same shape as the WebSocket `messageCreated` event):

```json
{
  "id": "9a7d…",
  "userId": "0b8e…",
  "channelId": "5f1c…",
  "content": "hello",
  "createdAt": "2026-11-02T16:04:05Z",
  "updatedAt": "2026-11-02T16:04:05Z"
}
```

- `userId` is the author, or **`null` if the author's account was deleted**: display it as "Deleted user". Such messages can no longer be edited or deleted by anyone.
- `updatedAt` differs from `createdAt` once the message was edited.

| Route | Description | Success |
|---|---|---|
| `GET /api/channels/{id}/messages` | Full history of a channel, oldest first | 200, array (`[]` if empty) / 404 channel |
| `POST /api/channels/{id}/messages` | Post a message | 201, message |
| `PATCH /api/channels/{id}/messages/{messageId}` | Edit (author only) | 200, message / 403 / 404 |
| `DELETE /api/channels/{id}/messages/{messageId}` | Delete (author only) | 204 / 403 / 404 |

Body for create and edit: `{ "content": "hello" }`, 1–2000 characters.

Notes:

- History is **not paginated** yet: the whole channel is returned. Timestamps have a one-second resolution, so messages posted within the same second may come back in any order.
- REST mutations are **not broadcast** to other clients. To send, edit or delete messages, use the WebSocket events ([websocket.md](websocket.md#chat)), which persist the change *and* notify everyone.
