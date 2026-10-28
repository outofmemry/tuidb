.PHONY: build test up down smoke web release docker
# Build the web app into internal/web/dist, which the Go binary embeds (`make web build`).
web:
	cd web && npm ci && npm run build
	touch internal/web/dist/.gitkeep
build:
	go build -o webtuidb .
test:
	go vet ./... && go test ./...
up:
	docker compose up -d --wait postgres mysql clickhouse redis mongo mariadb cassandra dynamodb && docker compose run --rm redis-seed
down:
	docker compose down -v
# Driver and end-to-end UI tests against the compose databases (needs `make up`).
smoke:
	WEBTUIDB_INTEGRATION=1 go test -count=1 ./...
# Cross-platform archives into dist/ without publishing (needs goreleaser).
release:
	goreleaser release --snapshot --clean
docker:
	docker build -t webtuidb --build-arg VERSION=$$(git describe --tags --always --dirty) .
