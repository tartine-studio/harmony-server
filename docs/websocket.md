# WebSocket protocol

Each client keeps **one** WebSocket connection to the server. Chat, presence and voice signaling all go through it.

## Connecting

```
GET /api/ws
Authorization: Bearer <accessToken>
```

Use `wss://` when the server is behind TLS. Clients that can't set headers on the handshake (browsers) can pass the token in the query string instead:

```
GET /api/ws?token=<accessToken>
```

Only **access** tokens are accepted. If the handshake is refused, the server answers with a plain-text HTTP error (not JSON) and no upgrade:

| HTTP | Meaning |
|---|---|
| 401 | Missing, invalid or expired token, refresh token used, or the user no longer exists. Refresh the token and retry; if refresh fails, log in again. |
| 500 | Server error. Retry with backoff. |

Once connected, the connection stays valid even after the access token expires.

### Sessions

Each connection is a **session**. A user can have several sessions at once (desktop + phone…). Every session receives broadcasts, and presence is per user (online while at least one session is open). Voice belongs to a single session: see [voice.md](voice.md).

### First frames

Right after the upgrade, the server always sends, in this order:

1. [`presenceSnapshot`](presence.md#presencesnapshot): who is online/idle, plus your own presence.
2. [`voiceStateSnapshot`](voice.md#voicestatesnapshot): who is in which voice channel.

Every frame after these is a live update to apply on top of them. When you reconnect, throw away your presence and voice state and rebuild it from the new snapshots.

## Frames

All frames are **text** frames containing one JSON object, the *envelope*:

```json
{
  "type": "eventName",
  "data": { },
  "requestId": "optional-client-chosen-id"
}
```

| Field | Description |
|---|---|
| `type` | Event name (`camelCase`), see the tables below |
| `data` | Event payload. Always send an object, even if empty: `{}` |
| `requestId` | Optional, client → server only. If the request fails, the error frame carries the same `requestId`. |

### Requests and acknowledgements

**Successful requests are not acknowledged.** You learn that a request worked when you receive its effect, which is usually a broadcast you get too (e.g. `sendMessage` → `messageCreated`). A failed request returns an `error` frame with your `requestId`. So the usual client pattern is:

- send with a fresh `requestId`, and show the action as pending;
- on `error` with that `requestId`: show the failure;
- on the matching broadcast: done.

### Errors

```json
{
  "type": "error",
  "requestId": "req-42",
  "data": { "error": "forbidden", "code": "FORBIDDEN" }
}
```

| `code` | Meaning |
|---|---|
| `PARSE_ERROR` | The frame is not valid JSON. No `requestId` (the server couldn't read it). |
| `UNKNOWN_EVENT` | Unknown `type` |
| `VALIDATION_ERROR` | Missing or invalid field in `data` |
| `NOT_FOUND` | Unknown channel or message |
| `FORBIDDEN` | Not the author of the message |
| `NOT_VOICE_CHANNEL` | `joinVoice` on a text channel |
| `VOICE_CHANNEL_FULL` | Voice channel at capacity |
| `NOT_IN_VOICE` | Voice action from a session that isn't in voice |
| `INVALID_SIGNALING` | Rejected SDP answer or ICE candidate |
| `INTERNAL_ERROR` | Server-side failure |

The connection stays open after an error.

### Limits and keepalive

| | Value | What the client must do |
|---|---|---|
| Ping | Server sends a WebSocket **ping** every 54 s | Answer with a **pong**. Browsers and most libraries do this automatically. |
| Read timeout | 60 s without a pong | The server closes the connection |
| Max frame size (client → server) | 64 KiB | Larger frames close the connection |
| Outgoing buffer | 256 frames per session | A client that doesn't read fast enough is disconnected |

The server doesn't need client-side pings. Every frame you send also counts as user [activity](presence.md#activity).

## Event reference

### Client → server

| `type` | `data` | Doc |
|---|---|---|
| `sendMessage` | `{ channelId, content }` | [Chat](#chat) |
| `editMessage` | `{ messageId, content }` | [Chat](#chat) |
| `deleteMessage` | `{ messageId }` | [Chat](#chat) |
| `activity` | `{}` | [Presence](presence.md#activity) |
| `presenceUpdate` | `{ invisible }` | [Presence](presence.md#presenceupdate) |
| `joinVoice` | `{ channelId }` | [Voice](voice.md#joinvoice) |
| `leaveVoice` | `{}` | [Voice](voice.md#leavevoice) |
| `updateVoiceState` | `{ muted?, deafened? }` | [Voice](voice.md#updatevoicestate) |
| `voiceAnswer` | `{ sdp }` | [Voice](voice.md#signaling) |
| `voiceCandidate` | `{ candidate, sdpMid, sdpMLineIndex, usernameFragment }` | [Voice](voice.md#signaling) |

### Server → client

| `type` | `data` | Sent to | Doc |
|---|---|---|---|
| `error` | `{ error, code }` | The requesting session | [Errors](#errors) |
| `messageCreated` | message | Everyone | [Chat](#chat) |
| `messageUpdated` | message | Everyone | [Chat](#chat) |
| `messageDeleted` | `{ messageId }` | Everyone | [Chat](#chat) |
| `presenceSnapshot` | `{ presences: [...] }` | New session | [Presence](presence.md#presencesnapshot) |
| `presenceBroadcast` | `{ userId, status, invisible? }` | Everyone (see doc) | [Presence](presence.md#presencebroadcast) |
| `voiceStateSnapshot` | `{ states: [...] }` | New session | [Voice](voice.md#voicestatesnapshot) |
| `voiceStateUpdate` | voice state | Everyone | [Voice](voice.md#voicestateupdate) |
| `voiceSpeaking` | `{ userId, speaking }` | Everyone | [Voice](voice.md#voicespeaking) |
| `voiceOffer` | `{ sdp }` | The voice session | [Voice](voice.md#signaling) |
| `voiceDisconnected` | `{ reason }` | The removed session | [Voice](voice.md#voicedisconnected) |

Ignore unknown event types and unknown fields: they may be added in future versions.

## Chat

### `sendMessage`

```json
{ "type": "sendMessage", "requestId": "r1", "data": { "channelId": "5f1c…", "content": "hello" } }
```

`channelId` and a non-empty `content` are required. Errors: `VALIDATION_ERROR`, `NOT_FOUND` (channel).

On success, **every** connected session (the sender's too) receives:

```json
{
  "type": "messageCreated",
  "data": {
    "id": "9a7d…",
    "userId": "0b8e…",
    "channelId": "5f1c…",
    "content": "hello",
    "createdAt": "2026-11-02T16:04:05Z",
    "updatedAt": "2026-11-02T16:04:05Z"
  }
}
```

In message history (REST), `userId` can be `null` when the author's account was deleted: display "Deleted user".

The broadcast does not carry your `requestId`. To match it to a pending message, compare `userId` with your own ID, plus `channelId` and `content`.

### `editMessage`

```json
{ "type": "editMessage", "requestId": "r2", "data": { "messageId": "9a7d…", "content": "hello!" } }
```

Author only. Errors: `VALIDATION_ERROR`, `NOT_FOUND`, `FORBIDDEN`. On success, everyone receives `messageUpdated` with the full updated message (same shape as `messageCreated`).

### `deleteMessage`

```json
{ "type": "deleteMessage", "requestId": "r3", "data": { "messageId": "9a7d…" } }
```

Author only. Errors: `VALIDATION_ERROR`, `NOT_FOUND`, `FORBIDDEN`. On success, everyone receives:

```json
{ "type": "messageDeleted", "data": { "messageId": "9a7d…" } }
```

`messageDeleted` has no `channelId`: look the message up by ID in your local state.

### Current behaviour to be aware of

- Chat events go to **all** sessions, whatever channel they are looking at. Filter by `channelId`.
- The server doesn't check the channel type: messages can be posted to voice channels. Clients should only offer chat in text channels.
- Over WebSocket, `content` must be non-empty, but its length is only bounded by the 64 KiB frame limit. The REST API enforces 2000 characters: apply the same limit client-side.
- Messages are persisted before being broadcast, so a reconnecting client can catch up with `GET /api/channels/{id}/messages`.
