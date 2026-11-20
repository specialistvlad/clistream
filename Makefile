.PHONY: build test e2e apple
build:
	go build -trimpath -o bin/ ./cmd/...
test:
	go test -race ./...
	go vet ./...
	python3 tests/requirements.py
e2e: build
	python3 tests/e2e.py
apple:
	apple/build-core.sh
	cd apple && xcodegen generate

.PHONY: dist
dist:
	python3 scripts/dist.py

.PHONY: test-deployment
test-deployment:
	python3 tests/deployment.py
