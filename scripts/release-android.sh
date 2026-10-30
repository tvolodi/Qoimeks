#!/usr/bin/env bash
# Builds the signed release APK and writes the files to publish into an output folder:
#   qoimeks-<version>.apk, qoimeks-<version>.apk.sha256 and latest.json.
# Fails instead of falling back to the debug key, and when the version code is not higher
# than the last published one. See README, "Release build".
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
mobile="${MOBILE_DIR:-$root/mobile}"
out="$root/../release-out"
base_url="https://ai-dala.com/qoimeks/"
last_published=""
first_release=0
dry_run=0
manifest_url=""
no_update_check=0
server_url=""
min_code=""
notes_ru=""
notes_kk=""
notes_file=""

usage() {
    cat <<USAGE
Usage: scripts/release-android.sh [options]
  --out DIR              output folder (default: ../release-out, outside git)
  --base-url URL         where the files are served (default: $base_url)
  --last-published N     last published version code, when DIR/latest.json does not exist
  --first-release        nothing was published before
  --min-version-code N   older versions must update (default: previous manifest value;
                         the current code with --first-release)
  --notes-ru TEXT        release notes in Russian
  --notes-kk TEXT        release notes in Kazakh
  --notes-file FILE      notes as lines "ru: ..." and "kk: ..." (instead of the two above)
  --manifest-url URL     manifest URL built into the app (default: <base-url>latest.json)
  --server-url URL       default server address built into the app (https; http only for
                         localhost or a private address)
  --no-update-check      build without the in-app update check (e.g. a Google Play build)
  --dry-run              run every check, print the build command, write nothing
USAGE
}

die() { echo "release-android: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --first-release|--dry-run|--no-update-check) ;;
        *) [ $# -ge 2 ] || die "$1 needs a value" ;;
    esac
    case "$1" in
        --out) out="$2"; shift 2 ;;
        --base-url) base_url="$2"; shift 2 ;;
        --last-published) last_published="$2"; shift 2 ;;
        --first-release) first_release=1; shift ;;
        --min-version-code) min_code="$2"; shift 2 ;;
        --notes-ru) notes_ru="$2"; shift 2 ;;
        --notes-kk) notes_kk="$2"; shift 2 ;;
        --notes-file) notes_file="$2"; shift 2 ;;
        --manifest-url) manifest_url="$2"; shift 2 ;;
        --server-url) server_url="$2"; shift 2 ;;
        --no-update-check) no_update_check=1; shift ;;
        --dry-run) dry_run=1; shift ;;
        *) usage >&2; die "unknown option $1" ;;
    esac
done

# --- signing key ---
[ -f "$mobile/android/key.properties" ] ||
    die "$mobile/android/key.properties is missing; a release signed with the debug key can never be updated by the real one"

# --- version ---
line=$(grep -E '^version:[[:space:]]*[0-9]+\.[0-9]+\.[0-9]+\+[0-9]+[[:space:]]*(#.*)?$' "$mobile/pubspec.yaml" || true)
[ -n "$line" ] || die "no 'version: X.Y.Z+N' in $mobile/pubspec.yaml"
version=$(echo "$line" | sed -E 's/^version:[[:space:]]*([0-9.]+)\+([0-9]+).*/\1/')
code=$(echo "$line" | sed -E 's/^version:[[:space:]]*([0-9.]+)\+([0-9]+).*/\2/')

# --- last published ---
manifest="$out/latest.json"
prev_min=""
if [ -f "$manifest" ]; then
    prev_code=$(grep -oE '"versionCode"[[:space:]]*:[[:space:]]*[0-9]+' "$manifest" | grep -oE '[0-9]+$' | head -n1 || true)
    [ -n "$prev_code" ] || die "no versionCode in $manifest"
    prev_min=$(grep -oE '"minVersionCode"[[:space:]]*:[[:space:]]*[0-9]+' "$manifest" | grep -oE '[0-9]+$' | head -n1 || true)
elif [ -n "$last_published" ]; then
    prev_code="$last_published"
elif [ "$first_release" = 1 ]; then
    prev_code=0
else
    die "cannot tell the last published version: $manifest does not exist; pass --last-published N or --first-release"
fi
case "$prev_code" in ''|*[!0-9]*) die "last published version code must be a number, got '$prev_code'" ;; esac
[ "$code" -gt "$prev_code" ] || die "version code $code is not higher than the last published $prev_code; raise it in pubspec.yaml"

if [ -z "$min_code" ]; then
    if [ -n "$prev_min" ]; then min_code="$prev_min"
    elif [ "$first_release" = 1 ]; then min_code="$code"
    else die "no previous minVersionCode to keep; pass --min-version-code N"
    fi
fi
case "$min_code" in ''|*[!0-9]*) die "--min-version-code must be a number, got '$min_code'" ;; esac

# --- notes ---
if [ -n "$notes_file" ]; then
    [ -f "$notes_file" ] || die "notes file $notes_file not found"
    [ -n "$notes_ru" ] || notes_ru=$(sed -n 's/^ru:[[:space:]]*//p' "$notes_file" | head -n1)
    [ -n "$notes_kk" ] || notes_kk=$(sed -n 's/^kk:[[:space:]]*//p' "$notes_file" | head -n1)
fi
[ -n "$notes_ru" ] || die "release notes in Russian are required (--notes-ru or --notes-file)"
[ -n "$notes_kk" ] || die "release notes in Kazakh are required (--notes-kk or --notes-file)"
for n in "$notes_ru" "$notes_kk"; do
    stripped=${n//[$'\n\r\t']/}
    clean=$(printf '%s' "$stripped" | LC_ALL=C tr -d '\000-\010\013\014\016-\037\177')
    [ "$clean" = "$stripped" ] || die "release notes contain a control character"
done

apk_name="qoimeks-$version.apk"
url="${base_url%/}/$apk_name"

# Flags passed to the build with --dart-define. The in-app update check adds its
# manifest URL here.
# DART-DEFINES: begin
dart_defines=()
if [ "$no_update_check" = 1 ]; then
    [ -z "$manifest_url" ] || die "--no-update-check and --manifest-url contradict each other"
else
    [ -n "$manifest_url" ] || manifest_url="${base_url%/}/latest.json"
    dart_defines+=("--dart-define=UPDATE_MANIFEST_URL=$manifest_url")
fi
if [ -n "$server_url" ]; then
    case "$server_url" in
        *[[:space:]]*) die "--server-url must not contain spaces, got '$server_url'" ;;
        https://?*) ;;
        http://?*)
            host=${server_url#http://}; host=${host%%[/?#]*}; host=${host##*@}; host=${host%:*}
            case "$host" in
                localhost|127.*|10.*|192.168.*|172.1[6-9].*|172.2[0-9].*|172.3[01].*) ;;
                *) die "--server-url $server_url: plain http is allowed only for localhost or a private address; use https" ;;
            esac ;;
        *) die "--server-url must be an https URL, got '$server_url'" ;;
    esac
    dart_defines+=("--dart-define=DEFAULT_SERVER_URL=$server_url")
fi
# DART-DEFINES: end

build=(flutter build apk --release --build-name "$version" --build-number "$code" ${dart_defines[@]+"${dart_defines[@]}"})

if [ "$dry_run" = 1 ]; then
    echo "dry run: version $version (code $code), last published $prev_code, minVersionCode $min_code"
    echo "would run in $mobile: ${build[*]}"
    echo "would write $out/$apk_name, $out/$apk_name.sha256, $out/latest.json (url $url)"
    exit 0
fi

# --- build ---
(cd "$mobile" && "${build[@]}")
built="$mobile/build/app/outputs/flutter-apk/app-release.apk"
[ -f "$built" ] || die "the build did not produce $built"

mkdir -p "$out"
cp "$built" "$out/$apk_name"
if command -v sha256sum >/dev/null 2>&1; then
    sum=$(sha256sum "$out/$apk_name" | cut -d' ' -f1)
else
    sum=$(shasum -a 256 "$out/$apk_name" | cut -d' ' -f1)
fi
echo "$sum  $apk_name" > "$out/$apk_name.sha256"

json() { # escapes a string for a JSON value
    local s=${1//'\'/'\\'}
    s=${s//'"'/'\"'}
    s=${s//$'\n'/'\n'}
    s=${s//$'\r'/}
    s=${s//$'\t'/'\t'}
    printf '%s' "$s"
}
cat > "$out/latest.json.tmp" <<JSON
{
  "versionCode": $code,
  "versionName": "$version",
  "url": "$(json "$url")",
  "sha256": "$sum",
  "minVersionCode": $min_code,
  "notes": {
    "ru": "$(json "$notes_ru")",
    "kk": "$(json "$notes_kk")"
  },
  "published": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON
mv "$out/latest.json.tmp" "$manifest"
echo "release $version (code $code) written to $out"
