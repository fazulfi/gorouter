#!/usr/bin/env bash
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fail() { printf 'PACKAGING_SMOKE=FAIL: %s\n' "$1" >&2; exit 1; }
require_file() { test -s "$root/$1" || fail "missing or empty artifact: $1"; }
for artifact in \
  packaging/windows/installer.wxs \
  packaging/windows/portable-tree.yaml \
  packaging/macos/Gorouter.app/Contents/Info.plist \
  packaging/macos/DMG_README.md \
  packaging/linux/gorouter.service \
  packaging/linux/gorouter.tarball.yaml \
  packaging/docker/Dockerfile \
  packaging/docker/compose.yaml \
  packaging/portable/portable-tree.yaml; do
  require_file "$artifact"
done
python - "$root" <<'PY'
from pathlib import Path
import plistlib, sys, xml.etree.ElementTree as ET
try:
    root = Path(sys.argv[1])
    ET.parse(root / "packaging/windows/installer.wxs")
    plistlib.load((root / "packaging/macos/Gorouter.app/Contents/Info.plist").open("rb"))
    import yaml
    for name in ("packaging/windows/portable-tree.yaml", "packaging/linux/gorouter.tarball.yaml", "packaging/portable/portable-tree.yaml", "packaging/docker/compose.yaml"):
        if not yaml.safe_load((root / name).read_text()):
            raise ValueError(f"empty YAML: {name}")
except Exception as exc:
    print(f"PACKAGING_SMOKE=FAIL: static validation: {exc}", file=sys.stderr)
    raise SystemExit(1)
PY
binary="${GOROUTER_BINARY:-$root/gorouter}"
if [ -f "$binary" ]; then
  test -x "$binary" || fail "binary is not executable: $binary"
  if ! "$binary" --version >/dev/null 2>&1; then
    "$binary" --help 2>&1 | grep -q '^Usage of' || fail "binary probe failed: $binary"
  fi
fi
if [ -f "$root/gorouter.exe" ]; then
  test -s "$root/gorouter.exe" || fail "empty Windows binary"
fi
printf 'PACKAGING_SMOKE=PASS\n'
