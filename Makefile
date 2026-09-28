.PHONY: setup dev run build test test-integration vet lint check docker-up docker-down docker-build migrate-up migrate-status migrate-create
PC = go tool -modfile=tools/go.mod process-compose

setup:
	@test -f config.yaml || cp config.example.yaml config.yaml
	go mod download
	go mod download -modfile=tools/go.mod

dev:
	$(PC) up -U --theme "One Dark"

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server
	go build -o bin/cli ./cmd/cli

test:
	go test -race ./...

test-integration:
	@test -n "$(TEST_DATABASE_URL)" || { echo "Set TEST_DATABASE_URL to a dedicated test database"; exit 1; }
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

check: vet test
	@test -z "$$(gofmt -l auth config handlers internal mailer middleware migrations models services ws cmd)"

docker-up:
	docker compose up -d --wait postgres

docker-down:
	docker compose down

docker-build:
	docker build -t meppvp-api .

migrate-up:
	go run ./cmd/cli migrate up

migrate-status:
	go run ./cmd/cli migrate status

migrate-create:
	@test -n "$(name)" || { echo "usage: make migrate-create name=add_widgets"; exit 1; }
	go run ./cmd/cli migrate create "$(name)"
