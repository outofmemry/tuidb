.PHONY: build test up down smoke
build:
	go build -o tuidb .
test:
	go vet ./... && go test ./...
up:
	docker compose up -d --wait postgres mysql clickhouse redis mongo mariadb cockroach cassandra dynamodb && docker compose up redis-seed
down:
	docker compose down -v
# Driver and end-to-end UI tests against the compose databases (needs `make up`).
smoke:
	TUIDB_INTEGRATION=1 go test -count=1 ./...
