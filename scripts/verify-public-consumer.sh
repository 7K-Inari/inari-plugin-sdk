#!/usr/bin/env bash
set -euo pipefail

: "${INARI_API_VERSION:?set INARI_API_VERSION to a public tag or commit}"
: "${INARI_PLUGIN_SDK_VERSION:?set INARI_PLUGIN_SDK_VERSION to a public tag or commit}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/inari-public-consumer.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

cp "$ROOT/testdata/public-consumer/consumer_test.go" "$TMP/consumer_test.go"
cd "$TMP"
export GOWORK=off
go mod init example.com/inari-public-consumer
go get "github.com/7K-Inari/inari-api@${INARI_API_VERSION}" "github.com/7K-Inari/inari-plugin-sdk@${INARI_PLUGIN_SDK_VERSION}"
go mod tidy
if grep -q '^replace ' go.mod; then
  echo "public consumer must not contain replace directives" >&2
  exit 1
fi
go test -race ./...
