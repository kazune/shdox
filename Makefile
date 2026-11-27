.DEFAULT_GOAL := build

BINARY ?= shdox

export CGO_ENABLED := 0
export GOAMD64 ?= v1
export GOARM64 ?= v8.0

.PHONY: fmt fmt-check lint test check build clean ci

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

check: lint fmt-check test

build:
	go build -trimpath -o "$(BINARY)" .

clean:
	rm -f "$(BINARY)"

ci: check
	bash scripts/ci.sh
