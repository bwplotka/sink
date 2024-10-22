#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

REPO_ROOT=$(realpath $(dirname "${BASH_SOURCE[0]}")/..)

MODPATH=${REPO_ROOT}/go
BINPATH="./sink"

# Print the command --help, but remove the full path (which is in tmp dir when
# build through go run).
cd ${MODPATH}
CGO_ENABLED=0 go run "${BINPATH}" -help 2>&1 >/dev/null | sed 's/^Usage of \/.*\//Usage of /'
