#!/bin/bash
# The Stella fork's release files in dist/: devvm-<version>-darwin-<arch>.tar.gz (the binary, README,
# CHANGELOG, LICENSE) for arm64 and amd64, and SHA256SUMS. The version is VERSION's.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
v=$(tr -d '[:space:]' < VERSION)
rm -rf dist && mkdir -p dist
for arch in arm64 amd64; do
  name="devvm-$v-darwin-$arch"
  stage="dist/$name"
  mkdir -p "$stage"
  GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/devvmhq/devvm/internal/cli.version=$v" -o "$stage/devvm" ./cmd/devvm
  cp README.md CHANGELOG.md LICENSE "$stage/"
  tar -C dist -czf "dist/$name.tar.gz" "$name"
  rm -rf "$stage"
done
(cd dist && shasum -a 256 ./*.tar.gz | sed 's| \./| |' > SHA256SUMS)
cat dist/SHA256SUMS
