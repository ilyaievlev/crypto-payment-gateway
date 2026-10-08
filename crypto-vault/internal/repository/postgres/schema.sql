CREATE SCHEMA IF NOT EXISTS crypto_vault;

CREATE TABLE IF NOT EXISTS crypto_vault.root_seeds (
    network TEXT PRIMARY KEY CHECK (network IN ('ETH', 'TRON', 'SOL', 'TON')),
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
