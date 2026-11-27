SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.ONESHELL:
.DEFAULT_GOAL := build

BINARY ?= shdox

export CGO_ENABLED := 0
export GOAMD64 ?= v1
export GOARM64 ?= v8.0

.PHONY: fmt fmt-check lint test build ci

fmt:
	gofmt -w .

fmt-check:
	@unformatted=$$(gofmt -l .)
	if [[ -n "$$unformatted" ]]; then
		printf 'Run make fmt to format:\n%s\n' "$$unformatted" >&2
		exit 1
	fi

lint:
	go vet ./...

test:
	go test ./...

build:
	go build -trimpath -o "$(BINARY)" .

ci: export GOOS := linux
ci: fmt-check lint test
	@workdir=$$(mktemp -d)
	trap 'rm -rf -- "$$workdir"' EXIT
	binary="$$workdir/shdox"
	go build -trimpath -o "$$binary" .

	export LC_ALL=C
	case "$$(go env GOARCH)" in
		amd64) expected_machine='Advanced Micro Devices X86-64' ;;
		arm64) expected_machine='AArch64' ;;
		*) printf 'Unsupported Linux CI architecture\n' >&2; exit 1 ;;
	esac
	machine=$$(readelf -hW "$$binary" | awk -F ': *' '/Machine:/ {print $$2}')
	if [[ "$$machine" != "$$expected_machine" ]]; then
		printf 'Unexpected ELF machine: %s (expected %s)\n' "$$machine" "$$expected_machine" >&2
		exit 1
	fi
	segments=$$(readelf -lW "$$binary")
	dynamic=$$(readelf -dW "$$binary")
	if grep -Eq '^[[:space:]]*INTERP[[:space:]]' <<< "$$segments" ||
	   grep -Fq '(NEEDED)' <<< "$$dynamic"; then
		printf 'Linux binary requires a dynamic loader or shared library\n' >&2
		exit 1
	fi

	cd "$$workdir" || exit 1
	printf '##\tCheck the file.\ncheck_file() { :; }\n' > script.sh
	"$$binary" check script.sh
	status=0
	"$$binary" fmt -d script.sh > format.diff || status=$$?
	test "$$status" -eq 1
	grep -Fxq '+## Check the file.' format.diff
	"$$binary" fmt -w script.sh
	"$$binary" fmt -d script.sh > format.diff
	test ! -s format.diff

	printf '%s\n' "# \`script.sh\`" '' '## Functions' '' "### \`check_file\`" '' \
		'Check the file.' > expected.md
	"$$binary" generate script.sh > actual.md
	diff -u expected.md actual.md
