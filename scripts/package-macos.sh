#!/usr/bin/env bash
# Seals a complete app bundle; ad-hoc signing does not provide notarization.
set -euo pipefail
if [[ $# -ne 2 ]]; then
  printf '用法：package-macos.sh <Clipnest.app> <输出.zip>\n' >&2
  exit 2
fi
clipnest_app=$1
clipnest_output=$2
if [[ ! -f "$clipnest_app/Contents/MacOS/Clipnest" ]]; then
  printf '应用包不存在或不完整。\n' >&2
  exit 1
fi
if [[ -e "$clipnest_output" ]]; then
  printf '输出文件已存在，请使用新的文件名。\n' >&2
  exit 1
fi
if [[ $(uname -s) == Darwin ]]; then
  /usr/bin/codesign --force --sign - "$clipnest_app"
  /usr/bin/codesign --verify --deep --strict "$clipnest_app"
  /usr/bin/ditto -c -k --keepParent "$clipnest_app" "$clipnest_output"
else
  clipnest_signer=${RCODESIGN:-rcodesign}
  "$clipnest_signer" --config-file /dev/null sign --timestamp-url none "$clipnest_app"
  python3 - "$clipnest_app" "$clipnest_output" <<'PY'
import pathlib,sys,zipfile
app=pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], 'x', compression=zipfile.ZIP_DEFLATED) as archive:
    for p in sorted(app.rglob('*')):
        archive.write(p,p.relative_to(app.parent))
PY
fi
printf '已生成临时签名预览包；未进行 Apple 公证。\n'
