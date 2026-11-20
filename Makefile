.PHONY: build test e2e apple dist test-dist test-deployment test-compatibility
build:
	go build -trimpath -o bin/ ./cmd/...
	$(RM) bin/clistream-supervise
test:
	go test -race ./cmd/... ./internal/... ./scripts/... ./tests/...
	go vet ./cmd/... ./internal/... ./scripts/... ./tests/...
e2e: build
	go test -tags=e2e -count=1 -timeout=15m ./tests/e2e
apple:
	apple/build-core.sh
	cd apple && xcodegen generate

dist:
	go run ./scripts/dist

ARCHIVE ?= dist/clistream-$(shell go env GOOS)-$(shell go env GOARCH).tar.gz
test-dist:
	go run ./scripts/dist verify "$(ARCHIVE)"

test-deployment: build
	go test -tags=deployment -count=1 ./tests/deployment

test-compatibility: build
	go test -tags=compatibility -count=1 ./tests/e2e
