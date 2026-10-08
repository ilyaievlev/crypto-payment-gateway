-- name: LoadEncryptedSeed :one
SELECT ciphertext
FROM crypto_vault.root_seeds
WHERE network = $1;

-- name: CreateEncryptedSeed :one
INSERT INTO crypto_vault.root_seeds (network, ciphertext)
VALUES ($1, $2)
ON CONFLICT (network) DO NOTHING
RETURNING network;

-- name: SeedExists :one
SELECT EXISTS (
    SELECT 1 FROM crypto_vault.root_seeds WHERE network = $1
) AS exists;

-- name: Ping :one
SELECT 1::integer AS alive;
