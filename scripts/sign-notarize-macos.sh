#!/usr/bin/env bash
# Developer ID signing + Apple notarization for Clipnest.
# Usage: APPLE_ID=you@example.com scripts/sign-notarize-macos.sh
# Needs: a "Developer ID Application" identity in the login keychain, and the
# app-specific password either already stored as notarytool profile
# "clipnest-notary" or stashed in keychain item "clipnest-notary-app-password".
set -euo pipefail
cd "$(dirname "$0")/.."

profile=clipnest-notary
stash=clipnest-notary-app-password

identity=$(security find-identity -v -p codesigning | sed -n 's/.*"\(Developer ID Application: .*\)"/\1/p' | head -1)
if [[ -z "$identity" ]]; then
  printf '钥匙串中没有 Developer ID Application 证书，请先下载并双击安装。\n' >&2
  exit 1
fi
team=$(printf '%s' "$identity" | sed -n 's/.*(\([A-Z0-9]\{10\}\))$/\1/p')
printf '签名身份：%s\nTeam ID：%s\n' "$identity" "$team"

if ! xcrun notarytool history --keychain-profile "$profile" >/dev/null 2>&1; then
  : "${APPLE_ID:?请设置 APPLE_ID（开发者账号的 Apple ID 邮箱）}"
  password=$(security find-generic-password -s "$stash" -w 2>/dev/null || true)
  if [[ -z "$password" ]]; then
    printf '找不到 App 专用密码（钥匙串项 %s）。\n' "$stash" >&2
    exit 1
  fi
  xcrun notarytool store-credentials "$profile" --apple-id "$APPLE_ID" --team-id "$team" --password "$password" >/dev/null
  unset password
  xcrun notarytool history --keychain-profile "$profile" >/dev/null
  security delete-generic-password -s "$stash" >/dev/null 2>&1 || true
  printf '公证凭据已保存为钥匙串配置 %s，临时密码项已删除。\n' "$profile"
fi

python3 - "$identity" "$profile" <<'PY'
import json, sys
p = "mygo.json"
c = json.load(open(p))
m = c.setdefault("macos", {})
m["signingIdentity"] = sys.argv[1]
m["notarize"] = {"keychainProfile": sys.argv[2]}
open(p, "w").write(json.dumps(c, ensure_ascii=False, indent=2) + "\n")
PY

rm -rf build/darwin-universal
go tool mygo build -platform darwin/universal .

app=build/darwin-universal/Clipnest.app
dmg=$(ls build/darwin-universal/*.dmg | head -1)
codesign --verify --deep --strict --verbose=2 "$app"
spctl -a -t exec -vv "$app"
spctl -a -t open --context context:primary-signature -vv "$dmg"
xcrun stapler validate "$dmg"
printf '完成：%s\n' "$dmg"
