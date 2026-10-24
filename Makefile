.PHONY: build test up down smoke web
# Build the web app into internal/web/dist, which the Go binary embeds (`make web build`).
web:
	cd web && npm ci && npm run build
	touch internal/web/dist/.gitkeep
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
