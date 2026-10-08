-- Keep in sync with deploy/migrations/payment-core.
CREATE SCHEMA IF NOT EXISTS payment_core;
-- Снимок таблицы индексатора нужен sqlc для проверки межсхемного запроса.
CREATE SCHEMA IF NOT EXISTS chain_worker;
CREATE TABLE IF NOT EXISTS chain_worker.tron_transfers (
    transaction_id TEXT NOT NULL,
    event_index INTEGER NOT NULL,
    block_number BIGINT NOT NULL,
    from_address TEXT NOT NULL,
    to_address TEXT NOT NULL,
    asset TEXT NOT NULL,
    amount NUMERIC(78, 0) NOT NULL,
    PRIMARY KEY (transaction_id, event_index)
);

CREATE SEQUENCE IF NOT EXISTS payment_core.tron_address_index_seq AS BIGINT MINVALUE 0 START 0;

CREATE TABLE IF NOT EXISTS payment_core.tron_invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT UNIQUE,
    key_index BIGINT NOT NULL UNIQUE CHECK (key_index >= 0),
    address TEXT NOT NULL UNIQUE,
    asset TEXT NOT NULL DEFAULT 'TRX' CHECK (asset = 'TRX'),
    expected_amount NUMERIC(78, 0) NOT NULL CHECK (expected_amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
