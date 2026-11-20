.PHONY: build test e2e apple
build:
	go build -trimpath -o bin/ ./cmd/...
test:
	go test -race ./...
	go vet ./...
e2e: build
	python3 tests/e2e.py
apple:
	apple/build-core.sh
	cd apple && xcodegen generate
