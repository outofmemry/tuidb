#!/usr/bin/env bash
# Loads the documentation demo data ("shop") into the compose test databases. Re-runnable.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
docker exec -i webtuidb-test-postgres-1 psql -U webtuidb -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = 'shop'" | grep -q 1 || docker exec -i webtuidb-test-postgres-1 psql -U webtuidb -d postgres -c "CREATE DATABASE shop" >/dev/null
docker exec -i webtuidb-test-postgres-1 psql -U webtuidb -d shop -v ON_ERROR_STOP=1 -q < "$here/seed/postgres-shop.sql"
docker exec -i webtuidb-test-mysql-1 mysql -uroot -proot < "$here/seed/mysql-shop.sql" 2>/dev/null
docker exec -i webtuidb-test-redis-1 redis-cli -a webtuidb --no-auth-warning < "$here/seed/redis-shop.txt" >/dev/null
docker exec -i webtuidb-test-mongo-1 mongosh -u webtuidb -p webtuidb --authenticationDatabase admin --quiet shop < "$here/seed/mongo-shop.js" >/dev/null
echo "seeded: postgres shop, mysql shop, redis, mongo shop"
