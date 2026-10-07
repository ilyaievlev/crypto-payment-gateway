-- +goose Up
CREATE SCHEMA IF NOT EXISTS payment_core;

-- +goose Down
-- No CASCADE: rollback must fail rather than remove business tables.
DROP SCHEMA payment_core;
