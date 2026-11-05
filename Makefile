.PHONY: db-up db-down test check web-build run seed

db-up:
	docker compose -f deploy/compose.yaml up -d --wait

db-down:
	docker compose -f deploy/compose.yaml down

test:
	export TEST_DATABASE_URL='postgres://commitpool:commitpool@localhost:55432/commitpool_test?sslmode=disable'; \
	go test -race ./...

# gofmt, vet, staticcheck and govulncheck, the last two at pinned versions.
check:
	gofmt -l . | (! grep .)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
	! go list -deps ./cmd/commitpool ./cmd/seed | grep -xE 'testing|.*/(paypaltest|storetest)'
	go vet -tags e2e ./cmd/commitpool

web-build:
	pnpm -C web install --frozen-lockfile
	pnpm -C web build

run:
	@set -a; if [ -f .env ]; then . ./.env; fi; set +a; \
	go run ./cmd/commitpool

seed:
	go run ./cmd/seed -base "$${COMMITPOOL_BASE_URL:-http://localhost:8080}"
