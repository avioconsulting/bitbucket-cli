#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
action=${1:?Expected linux, darwin or publish}
export PATH="$HOME/.local/bin:$PATH"
# Select a repository-specific Go version without changing agent defaults.
mise install go@1.26.8
eval "$(mise env go@1.26.8 --shell bash)"
version=$(cat AVIO_VERSION)
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-avio(-SNAPSHOT)?$ ]]
if [[ "$action" == publish ]]; then
    exec go run ./cmd/avio-publish
fi
case "$action:$(uname -s):$(uname -m)" in
    linux:Linux:x86_64) target=linux-x64; export CGO_ENABLED=0 ;;
    darwin:Darwin:arm64)
        target=darwin-arm64
        export CGO_ENABLED=1 MACOSX_DEPLOYMENT_TARGET=13.0
        export CGO_CFLAGS='-O2 -g -mmacosx-version-min=13.0'
        export CGO_LDFLAGS='-mmacosx-version-min=13.0'
        ;;
    *) echo 'Unsupported action or wrong build host' >&2; exit 1 ;;
esac
go version
go test ./...
make check-skills check-generated-skill
make -B build VERSION="$version" COMMIT="$(git rev-parse HEAD)" BUILD_DATE="$(git show -s --format=%cI HEAD)"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
root="bkt-$version"
mkdir -p "$stage/$root/bin" dist/avio
cp bin/bkt "$stage/$root/bin/"
cp LICENSE README.md CHANGELOG.md "$stage/$root/"
cp -R skills "$stage/$root/"
printf 'version=%s\nplatform=%s\ncommit=%s\n' "$version" "$target" "$(git rev-parse HEAD)" > "$stage/$root/package.properties"
archive="bkt-native-$target-$version.tar.gz"
tar -czf "dist/avio/$archive" -C "$stage" "$root"
mkdir "$stage/verify"
tar -xzf "dist/avio/$archive" -C "$stage/verify"
"$stage/verify/$root/bin/bkt" --version | grep -F -- "$version"
"$stage/verify/$root/bin/bkt" --help >/dev/null
if command -v sha256sum >/dev/null; then
    (cd dist/avio && sha256sum "$archive" > "$archive.sha256")
else
    (cd dist/avio && shasum -a 256 "$archive" > "$archive.sha256")
fi
