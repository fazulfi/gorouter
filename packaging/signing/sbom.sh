#!/usr/bin/env sh
set -eu

# Reproducible dependency SBOM: module paths and versions are emitted by Go.
# CI must retain the exact Go version and source revision alongside this file.
go list -m -json all
