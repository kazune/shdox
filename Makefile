.DEFAULT_GOAL := build

BINARY ?= shdox

export CGO_ENABLED := 0
export GOAMD64 ?= v1
export GOARM64 ?= v8.0

.PHONY: fmt fmt-check lint test build ci

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .) || exit; \
	if [ -n "$$unformatted" ]; then \
		printf 'Run make fmt to format:\n%s\n' "$$unformatted" >&2; \
		exit 1; \
	fi

lint:
	go vet ./...

test:
	go test ./...

build:
	go build -trimpath -o "$(BINARY)" .

ci: export GOOS := linux
ci: fmt-check lint test
	bash scripts/ci.sh
