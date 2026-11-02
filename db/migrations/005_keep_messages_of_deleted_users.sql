-- Deleting a user keeps their messages: the author becomes NULL ("Deleted user").
-- SQLite can't alter a foreign key, so the table is rebuilt.

-- +goose Up
CREATE TABLE messages_new (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE SET NULL,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT INTO messages_new (id, user_id, channel_id, content, created_at, updated_at)
    SELECT id, user_id, channel_id, content, created_at, updated_at FROM messages;
DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;

-- +goose Down
CREATE TABLE messages_old (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id),
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
INSERT INTO messages_old (id, user_id, channel_id, content, created_at, updated_at)
    SELECT id, user_id, channel_id, content, created_at, updated_at FROM messages WHERE user_id IS NOT NULL;
DROP TABLE messages;
ALTER TABLE messages_old RENAME TO messages;
