#!/usr/bin/env bash
set -euo pipefail

workdir=$(mktemp -d)
trap 'rm -rf -- "$workdir"' EXIT
binary="$workdir/shdox"
go build -trimpath -o "$binary" .

export LC_ALL=C
case "$(go env GOARCH)" in
	amd64) expected_machine='Advanced Micro Devices X86-64' ;;
	arm64) expected_machine='AArch64' ;;
	*) printf 'Unsupported Linux CI architecture\n' >&2; exit 1 ;;
esac
machine=$(readelf -hW "$binary" | awk -F ': *' '/Machine:/ {print $2}')
if [[ "$machine" != "$expected_machine" ]]; then
	printf 'Unexpected ELF machine: %s (expected %s)\n' "$machine" "$expected_machine" >&2
	exit 1
fi
segments=$(readelf -lW "$binary")
dynamic=$(readelf -dW "$binary")
if grep -Eq '^[[:space:]]*INTERP[[:space:]]' <<< "$segments" ||
   grep -Fq '(NEEDED)' <<< "$dynamic"; then
	printf 'Linux binary requires a dynamic loader or shared library\n' >&2
	exit 1
fi

cd "$workdir" || exit 1
run_success() {
	local command=$1 output=$2
	if ! "$binary" "$command" script.sh > "$output" 2> diagnostics || [[ -s diagnostics ]]; then
		cat diagnostics >&2
		return 1
	fi
}

printf '##\tCheck the file.\ncheck_file() { :; }\n' > script.sh
run_success check check.before
test ! -s check.before
run_success generate before.md
run_success fmt formatted.sh
status=0
"$binary" fmt -d script.sh > format.diff || status=$?
test "$status" -eq 1
grep -Fxq '+## Check the file.' format.diff
"$binary" fmt -w script.sh
cmp formatted.sh script.sh
run_success check check.after
cmp check.before check.after
run_success fmt reformatted.sh
cmp formatted.sh reformatted.sh
"$binary" fmt -d script.sh > format.diff
test ! -s format.diff

cat > expected.md <<'MARKDOWN'
# `script.sh`

## Functions

### `check_file`

Check the file.
MARKDOWN
run_success generate actual.md
cmp before.md actual.md
diff -u expected.md actual.md
