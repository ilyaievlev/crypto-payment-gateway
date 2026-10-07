#!/bin/sh
# Run from the repository root after make bootstrap.
set -eu
make generate
make check
make build
# Generated artifacts must be committed and reproducible.
git diff --exit-code -- go.mod go.sum pkg/proto api-gateway/internal/delivery/http/docs payment-core/internal/repository/postgres/db '*health_mock_test.go'
if [ -n "${BUF_BASE:-}" ]; then
  make breaking BUF_BASE="$BUF_BASE"
fi
