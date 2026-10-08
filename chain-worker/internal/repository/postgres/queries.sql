-- name: GetTRONScanHeight :one
SELECT last_block_number
FROM chain_worker.tron_scan_state
WHERE singleton = TRUE;

-- name: CreateTRONScanState :execrows
INSERT INTO chain_worker.tron_scan_state (singleton, last_block_number)
VALUES (TRUE, $1)
ON CONFLICT (singleton) DO NOTHING;

-- name: InsertTRONBlock :execrows
INSERT INTO chain_worker.tron_blocks (block_number, block_id)
VALUES ($1, $2)
ON CONFLICT (block_number) DO NOTHING;

-- name: GetTRONBlockID :one
SELECT block_id
FROM chain_worker.tron_blocks
WHERE block_number = $1;

-- name: InsertTRONTransfer :execrows
INSERT INTO chain_worker.tron_transfers (
    transaction_id, event_index, block_number, from_address, to_address, asset, amount
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (transaction_id, event_index) DO NOTHING;

-- name: AdvanceTRONScanHeight :execrows
UPDATE chain_worker.tron_scan_state
SET last_block_number = $1, updated_at = now()
WHERE singleton = TRUE AND last_block_number = $2;

-- name: ListTRONTransfersTo :many
SELECT transaction_id, event_index, block_number, from_address, to_address, asset, amount::text AS amount
FROM chain_worker.tron_transfers
WHERE to_address = $1
ORDER BY block_number, transaction_id, event_index
LIMIT $2;
