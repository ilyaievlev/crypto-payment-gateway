-- Keep in sync with deploy/migrations/chain-worker.
CREATE SCHEMA IF NOT EXISTS chain_worker;

CREATE TABLE IF NOT EXISTS chain_worker.tron_blocks (
    block_number BIGINT PRIMARY KEY CHECK (block_number > 0),
    block_id TEXT NOT NULL UNIQUE,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chain_worker.tron_transfers (
    transaction_id TEXT NOT NULL CHECK (transaction_id ~ '^[0-9a-f]{64}$'),
    event_index INTEGER NOT NULL CHECK (event_index >= 0),
    block_number BIGINT NOT NULL REFERENCES chain_worker.tron_blocks(block_number),
    from_address TEXT NOT NULL,
    to_address TEXT NOT NULL,
    asset TEXT NOT NULL DEFAULT 'TRX',
    amount NUMERIC(78, 0) NOT NULL CHECK (amount > 0),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (transaction_id, event_index)
);

CREATE TABLE IF NOT EXISTS chain_worker.tron_scan_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    last_block_number BIGINT NOT NULL CHECK (last_block_number >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS tron_transfers_recipient_idx
    ON chain_worker.tron_transfers (to_address, block_number);
