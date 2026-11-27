#!/usr/bin/env bash
set -euo pipefail

tag=${GITHUB_REF_NAME:-}
if [[ ${GITHUB_REF_TYPE:-} != tag || ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
	printf 'Expected a semantic version tag, got %q\n' "$tag" >&2
	exit 1
fi

case ${GOOS:-}/${GOARCH:-} in
	linux/amd64 | linux/arm64 | darwin/amd64 | darwin/arm64) ;;
	*) printf 'Unsupported release target: %s/%s\n' "${GOOS:-}" "${GOARCH:-}" >&2; exit 1 ;;
esac

: "${RELEASE_ASSET_DIR:?RELEASE_ASSET_DIR is required}"
version=${tag#v}
package_name="shdox_${version}_${GOOS}_${GOARCH}"
workdir=$(mktemp -d)
trap 'rm -rf -- "$workdir"' EXIT
package_dir="$workdir/$package_name"
mkdir -p "$package_dir/docs" "$RELEASE_ASSET_DIR"

binary="$package_dir/shdox"
make build BINARY="$binary"
actual_version=$("$binary" --version)
if [[ $actual_version != "shdox $version" ]]; then
	printf 'Unexpected version: %s (expected shdox %s)\n' "$actual_version" "$version" >&2
	exit 1
fi

cp LICENSE README.md "$package_dir/"
cp docs/shdox.md "$package_dir/docs/"
tar -czf "$RELEASE_ASSET_DIR/$package_name.tar.gz" -C "$workdir" "$package_name"
