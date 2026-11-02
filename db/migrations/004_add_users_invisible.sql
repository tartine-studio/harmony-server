-- +goose Up
ALTER TABLE users ADD COLUMN invisible INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE users DROP COLUMN invisible;
