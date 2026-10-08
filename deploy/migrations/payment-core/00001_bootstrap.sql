-- +goose Up
CREATE SCHEMA IF NOT EXISTS payment_core;
CREATE SEQUENCE payment_core.tron_address_index_seq AS BIGINT MINVALUE 0 START 0;
CREATE TABLE payment_core.tron_invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT UNIQUE,
    key_index BIGINT NOT NULL UNIQUE CHECK (key_index >= 0),
    address TEXT NOT NULL UNIQUE,
    asset TEXT NOT NULL DEFAULT 'TRX' CHECK (asset = 'TRX'),
    expected_amount NUMERIC(78, 0) NOT NULL CHECK (expected_amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE payment_core.tron_invoices;
DROP SEQUENCE payment_core.tron_address_index_seq;
-- No CASCADE: rollback must fail rather than remove business tables.
DROP SCHEMA payment_core;
