-- name: Ping :one
SELECT 1::integer AS alive;

-- name: GetNextTRONAddressIndex :one
SELECT nextval('payment_core.tron_address_index_seq')::bigint;

-- name: FindTRONInvoiceByIdempotencyKey :one
SELECT id::text, key_index, address, asset, expected_amount::text, created_at
FROM payment_core.tron_invoices
WHERE idempotency_key = $1;

-- name: CreateTRONInvoice :one
INSERT INTO payment_core.tron_invoices (idempotency_key, key_index, address, expected_amount)
VALUES (NULLIF($1::text, ''), $2, $3, $4)
ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
RETURNING id::text, key_index, address, asset, expected_amount::text, created_at;

-- name: GetTRONInvoiceByID :one
SELECT id::text, key_index, address, asset, expected_amount::text, created_at
FROM payment_core.tron_invoices
WHERE id::text = $1;

-- name: ListTRONInvoiceTransfers :many
SELECT t.transaction_id, t.event_index, t.block_number, t.from_address,
       t.to_address, t.asset, t.amount::text
FROM payment_core.tron_invoices i
JOIN chain_worker.tron_transfers t ON t.to_address = i.address
WHERE i.id::text = $1 AND t.asset = i.asset
ORDER BY t.block_number, t.transaction_id, t.event_index;
