#!/bin/sh
# Run after make apps. Requires curl and Docker Compose.
set -eu
for port in 18080 8081 8082 8083; do
  for endpoint in healthz readyz; do
    curl --fail --silent --show-error --max-time 5 --output /dev/null "http://127.0.0.1:$port/$endpoint"
  done
done
curl --fail --silent --show-error --max-time 5 --output /dev/null http://127.0.0.1:18080/swagger/doc.json
if docker compose version >/dev/null 2>&1; then
  docker compose --env-file .env -f deploy/docker-compose.yml exec -T crypto-vault /server -healthcheck
else
  docker-compose --env-file .env -f deploy/docker-compose.yml exec -T crypto-vault /server -healthcheck
fi
printf 'HTTP health/readiness, Swagger and internal vault health: OK\n'
