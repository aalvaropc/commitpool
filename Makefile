.PHONY: db-up db-down test web-build run seed

db-up:
	docker compose -f deploy/compose.yaml up -d --wait

db-down:
	docker compose -f deploy/compose.yaml down

test:
	export TEST_DATABASE_URL='postgres://commitpool:commitpool@localhost:55432/commitpool_test?sslmode=disable'; \
	go test ./...

web-build:
	pnpm -C web install --frozen-lockfile
	pnpm -C web build

run:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; \
	go run ./cmd/commitpool

seed:
	go run ./cmd/seed -base "$${COMMITPOOL_BASE_URL:-http://localhost:8080}"
