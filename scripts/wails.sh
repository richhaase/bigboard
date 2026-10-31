#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" == Darwin ]]; then
  export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=13.0"
  export CGO_CXXFLAGS="${CGO_CXXFLAGS:--O2 -g} -mmacosx-version-min=13.0"
  export CGO_LDFLAGS="${CGO_LDFLAGS:--O2 -g} -mmacosx-version-min=13.0"
fi
exec go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.0 "$@"
