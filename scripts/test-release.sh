#!/usr/bin/env bash
# Tests scripts/release-android.sh with a stub `flutter` and a temp copy of the pubspec.
set -uo pipefail
script="$(cd "$(dirname "$0")" && pwd)/release-android.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

mkdir -p "$tmp/bin" "$tmp/mobile/android"
cat > "$tmp/bin/flutter" <<'STUB'
#!/usr/bin/env bash
echo "$@" > "$STUB_LOG"
mkdir -p build/app/outputs/flutter-apk
echo "fake apk" > build/app/outputs/flutter-apk/app-release.apk
STUB
chmod +x "$tmp/bin/flutter"
printf 'name: x\nversion: 1.2.3+7\n' > "$tmp/mobile/pubspec.yaml"
export PATH="$tmp/bin:$PATH" MOBILE_DIR="$tmp/mobile" STUB_LOG="$tmp/flutter.log"

check() { # name, condition exit status
    if [ "$2" -eq 0 ]; then echo "ok   $1"; else echo "FAIL $1"; fails=$((fails + 1)); fi
}
run() { bash "$script" "$@" >"$tmp/stdout" 2>"$tmp/stderr"; }
notes=(--notes-ru $'Новое "в" версии\nСтрока\t2 C:\\x' --notes-kk "Жаңа нұсқа")

# no key.properties
run --out "$tmp/o1" --first-release "${notes[@]}"; rc=$?
[ $rc -ne 0 ] && grep -q key.properties "$tmp/stderr" && [ ! -e "$tmp/o1" ] && [ ! -e "$tmp/flutter.log" ]
check "missing key.properties fails, builds nothing" $?
echo "storeFile=x" > "$tmp/mobile/android/key.properties"

# neither last-published source
run --out "$tmp/o2" "${notes[@]}"; rc=$?
[ $rc -ne 0 ] && [ ! -e "$tmp/o2" ]; check "no last-published source fails" $?

# code not higher
run --out "$tmp/o2" --last-published 7 "${notes[@]}"; rc=$?
[ $rc -ne 0 ] && [ ! -e "$tmp/flutter.log" ]; check "code equal to last published fails" $?

# notes required
run --out "$tmp/o2" --first-release; rc=$?
[ $rc -ne 0 ]; check "notes are required" $?

# dry run
run --out "$tmp/o3" --first-release --dry-run "${notes[@]}"; rc=$?
[ $rc -eq 0 ] && grep -q "flutter build apk --release --build-name 1.2.3 --build-number 7" "$tmp/stdout" \
    && [ ! -e "$tmp/o3" ] && [ ! -e "$tmp/flutter.log" ]
check "dry run prints the command and writes nothing" $?

# update check define
d=--dart-define=UPDATE_MANIFEST_URL
run --out "$tmp/o3" --first-release --dry-run "${notes[@]}"
grep -qF -- "$d=https://ai-dala.com/qoimeks/latest.json" "$tmp/stdout"; check "default manifest url define" $?
run --out "$tmp/o3" --first-release --dry-run --base-url https://example.org/app "${notes[@]}"
grep -qF -- "$d=https://example.org/app/latest.json" "$tmp/stdout"; check "--base-url derives the manifest url" $?
run --out "$tmp/o3" --first-release --dry-run --manifest-url https://x.org/y.json "${notes[@]}"
grep -qF -- "$d=https://x.org/y.json" "$tmp/stdout"; check "--manifest-url overrides" $?
run --out "$tmp/o3" --first-release --dry-run --no-update-check "${notes[@]}"; rc=$?
[ $rc -eq 0 ] && ! grep -q UPDATE_MANIFEST_URL "$tmp/stdout"; check "--no-update-check adds no define" $?
run --out "$tmp/o3" --first-release --dry-run --no-update-check --manifest-url https://x.org/y.json "${notes[@]}"; rc=$?
[ $rc -ne 0 ]; check "--no-update-check with --manifest-url fails" $?

# first release
out="$tmp/o4"
run --out "$out" --first-release "${notes[@]}"; rc=$?
[ $rc -eq 0 ]; check "first release succeeds" $?
apk="$out/qoimeks-1.2.3.apk"
[ -f "$apk" ]; check "apk written" $?
(cd "$out" && sha256sum -c qoimeks-1.2.3.apk.sha256 >/dev/null 2>&1); check "sha256 file matches the apk" $?
m="$out/latest.json"
ok=0
for pat in '"versionCode": 7' '"versionName": "1.2.3"' '"url": "https://ai-dala.com/qoimeks/qoimeks-1.2.3.apk"' \
    '"minVersionCode": 7' '"ru": "Новое \\"в\\" версии\\nСтрока\\t2 C:\\\\x"' '"kk": "Жаңа нұсқа"' '"published": "[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]{8}Z"'; do
    grep -Eq "$pat" "$m" || { echo "  manifest lacks: $pat"; ok=1; }
done
sum=$(cut -d' ' -f1 "$out/qoimeks-1.2.3.apk.sha256")
grep -q "\"sha256\": \"$sum\"" "$m" || ok=1
check "manifest has every field" $ok

# next release: same code now fails, higher code keeps minVersionCode
run --out "$out" "${notes[@]}"; rc=$?
[ $rc -ne 0 ]; check "republishing the same code fails" $?
printf 'name: x\nversion: 1.2.4+8\n' > "$tmp/mobile/pubspec.yaml"
run --out "$out" --base-url https://example.org/app "${notes[@]}"; rc=$?
[ $rc -eq 0 ] && grep -q '"url": "https://example.org/app/qoimeks-1.2.4.apk"' "$out/latest.json" \
    && grep -q '"minVersionCode": 7' "$out/latest.json" && [ -f "$out/qoimeks-1.2.3.apk" ]
check "next release honours --base-url, keeps minVersionCode and the old apk" $?
run --out "$out" --min-version-code 8 --notes-file /dev/null; rc=$?
[ $rc -ne 0 ]; check "empty notes file fails" $?

[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
