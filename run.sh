#!/usr/bin/env bash
# viam-server entrypoint for the Crestron module.
#
# The controller resolves the Web API token from CRESTRON_ENV (a KEY=value file);
# default it to erh's file so the token never has to live in the cloud config.
# viam-server runs as root and the file is world-readable, so this resolves.
set -euo pipefail
cd "$(dirname "$0")"
export CRESTRON_ENV="${CRESTRON_ENV:-/home/erh/.crestron.env}"

bin=./bin/crestron-module
# Rebuild if the binary is missing or any Go source is newer than it.
if [ ! -x "$bin" ] || [ -n "$(find . -name '*.go' -newer "$bin" -print -quit 2>/dev/null)" ]; then
  go build -o "$bin" ./cmd/module
fi
exec "$bin" "$@"
