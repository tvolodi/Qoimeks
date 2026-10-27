# iOS study: the cheapest working way to run Qoimeks on an iPhone

Task T-0017, owner decision O-21 (the warehouse master has an iPhone). Docs only: no code,
pubspec or server was changed.

Every statement carries one mark:

- **VERIFIED** — checked by running, or by reading the code or package source (repo state f340f2d).
- **SOURCED** — taken from the cited public page.
- **UNVERIFIED** — must be tested on the iPhone.

## 1. Question and recommendation

**Question:** what is the cheapest way to get the Qoimeks app working on an iPhone?

**Recommendation:** try option A first: the Flutter web build installed as a web app (PWA)
from an HTTPS address. It costs no Apple fee and needs no account. It reuses the existing
Flutter code, and the owner can test it in one afternoon on their iPhone. Together with the
test, run one small code task: choose the voice encoder at run time, and fix the web
manifest. Decide on option B (native app, Apple Developer Program, TestFlight) only if the
test shows camera or voice failing both in the home-screen mode and in a Safari tab.

What does not exist yet: a build served at an HTTPS URL. The owner cannot start the test
until we (with infra) provide one (section 4).

## 2. Option A: Flutter web build as an installable web app (PWA)

### What exists today

- `flutter build web --release` of the current `mobile/` (f340f2d) succeeds in 57 s and gives
  about 37 MB in `build/web` (`main.dart.js` 3.1 MB). **VERIFIED**
- The repo already has `mobile/web` (index.html, manifest.json, icons) and `mobile/ios`. **VERIFIED**
- The web manifest is still the Flutter default: name and short_name "qoimeks", description
  "A new Flutter project", theme and background colour #0175C2 (not the design system), default
  icons. `index.html` has the `apple-mobile-web-app-title` and `apple-touch-icon` tags. **VERIFIED**
- By default the bootstrap loads CanvasKit from gstatic.com (flutter-canvaskit). **VERIFIED**
  The build can be made self-contained with the flag `--no-web-resources-cdn`; the flag name
  comes from Flutter and is **to be confirmed**.
- Server CORS already allows any origin (`Access-Control-Allow-Origin: *`, headers
  Content-Type and Authorization, `server/internal/api/api.go`), so a web app served from
  another origin can call the API. **VERIFIED**
- The app asks the user for the server address and the access code and stores them with
  shared_preferences (on web: browser local storage). **VERIFIED**
- The mobile code has `kIsWeb` branches in `lib/voice.dart` only. **VERIFIED**

### Platform facts

- Camera and microphone (getUserMedia) are supported in iOS web apps since iOS 13. **SOURCED**
  (firt.dev/notes/pwa-ios)
- Camera stream failures or freezes were reported in the home-screen (standalone) mode on some
  iOS versions while the same page works in Safari (WebKit bugs 273938, 275527, 252465,
  2023–2024 reports). Developer workaround: remove the `apple-mobile-web-app-capable` meta tag /
  manifest `display: standalone`, so the page runs in Safari with a home-screen icon.
  Microphone-specific reports were not found. **SOURCED**
- Safari records audio as audio/mp4 (AAC) since iOS 14.5; WebM with Opus recording was added in
  Safari/iOS 18.4 (secondary sources, unverified against Apple notes); use
  `MediaRecorder.isTypeSupported` to detect. **SOURCED** (addpipe.com media recorder demo and a
  testing guide, both third-party)

### Costs

- No Apple fee and no account: option A does not use the Apple Developer Program (for its
  fee see section 3). **UNVERIFIED** (a cost estimate from the task brief, not tested)
- Reuse of the existing Flutter code. **VERIFIED** (the repo has `mobile/web` and the web build
  succeeds, fact a)
- Work needed (work items from the task brief):
  - manifest and icons in the design system; **VERIFIED** that they are still the Flutter defaults
  - voice encoder choice (below); **VERIFIED** (code reading)
  - the in-app update check switched off on web (below); **UNVERIFIED** how it behaves on web
  - self-hosted CanvasKit and zxing files (optional); **UNVERIFIED** (flag and option names to be
    confirmed)
  - a hosting place for 37 MB of static files, **VERIFIED** (size of `build/web`): the apex nginx
    download area `/var/www/downloads/qoimeks/` or a new subpath, on infra's side; **UNVERIFIED**
    (not checked with infra)
  - service worker caching to be designed; updating works by reloading. **UNVERIFIED**
- Install is manual, by Share > Add to Home Screen; there is no store. **UNVERIFIED** (must be
  tested on the iPhone)

### Code changes option A needs

1. **Voice (required).** `lib/voice.dart` on web hard-codes `AudioEncoder.opus` (webm
   container, file name `phrase.webm`). The record_web package (1.3.0) then asks
   `MediaRecorder.isTypeSupported` for `audio/webm; codecs=opus`, `audio/opus; codecs=opus`,
   `audio/opus` and fails if none is supported. Its AAC list is `audio/mp4;codecs=mp4a`,
   `audio/mp4;codecs=mp4a.40.2`, `audio/aac`. The server passes the uploaded file name to the
   speech service (`stt.go`). So voice on iPhone needs a code change: choose the encoder at run
   time (AAC when opus is not supported, file name `phrase.m4a`), then test. **VERIFIED**
2. **Manifest and icons (required).** Replace the Flutter defaults with the design-system
   name, description, colours and icons. **VERIFIED** (current state, see above)
3. **Update check off on web (required).** The in-app APK update check is Android only and must
   be switched off on web: it reads package info and offers an APK. **VERIFIED** that it is
   Android only; **UNVERIFIED** how it behaves on web.
4. **Self-contained assets (optional).** CanvasKit via `--no-web-resources-cdn` (to be
   confirmed) so the app does not depend on gstatic.com.

### Risks of option A

- Standalone-mode camera bugs (WebKit reports above). **SOURCED**
- Voice format (above). **VERIFIED** for the code, **UNVERIFIED** on the phone.
- iOS may clear web storage for sites not used in 7 days when the site is not installed.
  **UNVERIFIED**; test it. If it happens, the address and the code must be re-entered, for
  example through the setup QR.

## 3. Option B: native iOS app through the Apple Developer Program and TestFlight

- The Apple Developer Program costs 99 USD per membership year; TestFlight is a membership
  benefit. **SOURCED** (developer.apple.com/programs)
- A free Apple account can run an app on the person's own device through Xcode only. **SOURCED**
  (same page)
- Building an iOS app needs Xcode, so a Mac or a cloud macOS build service. **SOURCED** (same
  page). The price of a cloud macOS build service was not researched.
- Apple review is not needed for TestFlight internal testers. **UNVERIFIED**
- Signing and provisioning work is needed, and the owner's Apple identity is needed: an
  owner-only step. **UNVERIFIED**
- `mobile/ios` already exists in the repo. **VERIFIED** No iOS build was made; whether it builds
  is **UNVERIFIED**.

## 4. What the owner does on the iPhone to test option A

**First, the owner needs from us:** a build served at an HTTPS URL. This does not exist yet.
The test URL is to be provided by infra, on a staging path. Nothing below can start before it.

Steps (Safari; write down the iOS version first):

1. Open the test URL in Safari.
2. Enter the server address and the access code (or scan the setup QR).
3. Camera: allow the permission, scan a barcode (EAN-13 or Code 128) and a QR.
4. Voice: record a phrase and check that it is recognised.
5. Add to the home screen (Share > Add to Home Screen), open it from the icon, repeat steps 3 and 4.
6. Reopen the app after a day: check that the address and the code are still there.
7. Report the iOS version and what failed, per step and per mode (Safari tab / home screen).

If steps 3–4 fail in the home-screen mode but work in a Safari tab, the fallback is the
Safari-tab workaround from section 2 (**SOURCED**, developer workaround).

## 5. What the codes feature (docs/codes-design.md) needs on web

- `mobile_scanner` 7.4.2 supports web with three backends. The default, "auto", uses the
  browser's native BarcodeDetector when present and otherwise zxing-wasm. **VERIFIED**
- zxing-wasm is about 2 MB of WASM loaded from cdn.jsdelivr.net on first use, a third-party
  request. It can be self-hosted via `alternateScriptUrl` (to be confirmed). **VERIFIED** for
  size and source; the self-hosting option is **to be confirmed**.
- The package README says the native BarcodeDetector is available in Safari 17+. The earlier
  note that Safari has none is therefore outdated for current iOS. **SOURCED** (package
  README). The formats actually detected vary by OS (README footnote); EAN-13, Code 128 and QR
  are all in the native list. **SOURCED**
- Whether scanning works on the owner's iPhone, in both modes: **UNVERIFIED** (test steps 3 and 5).
- The QR setup scanner (`lib/setup_qr.dart`, `qr_scanner.dart`) uses `mobile_scanner`, so it
  would use the same web backends. **VERIFIED**
- Camera in the home-screen mode: see the WebKit reports in section 2. **SOURCED**

## 6. Open questions

- Which iOS version is on the warehouse master's iPhone? It decides BarcodeDetector (Safari 17+)
  and the Opus/AAC recording case. **UNVERIFIED**
- Where does the test build live (staging path), and who serves the 37 MB? Infra's side.
- Does camera work in the home-screen mode on that phone, and does voice work in either mode?
  **UNVERIFIED**
- Is web storage kept for the installed web app after a week without use? **UNVERIFIED**
- Should the web build be self-contained (CanvasKit, zxing-wasm) to avoid third-party requests?
  Flag and option names are to be confirmed.
- How should the web app update (service worker caching to be designed)?
- Cost of a cloud macOS build service for option B: not researched.
- Does a TestFlight internal build avoid Apple review? **UNVERIFIED**

## Status (T-0019)

- The web build is prepared for the iPhone: the voice encoder is chosen at run time (Opus/WebM
  where the recorder supports it, else AAC/M4A), the web manifest and `index.html` carry the
  Qoimeks name, description and design colours (`#14304f` theme, `#f6f1e7` background), and the
  APK update check is off on web. The design-system icons do not exist yet; the default icons stay.
- `scripts/build-web.sh` runs `flutter build web --release --no-web-resources-cdn`, so CanvasKit is
  served from the app and not from gstatic.com. The flag exists in the installed Flutter.
- zxing-wasm is not self-hosted: the QR/barcode fallback loads from the jsDelivr CDN, and only on
  browsers without `BarcodeDetector` (Safari 17+ uses the native detector and loads nothing).

### Cache-busting of the web build (T-0021)

Cloudflare tells browsers to reuse `.js`, fonts and images for up to 4 hours, and infra cannot
change that for now. `scripts/build-web.sh` therefore runs `mobile/tool/cache_bust.dart` after
the build: `flutter_bootstrap.js` loads `main.dart.js?v=<id>`, where the id is the first 12 hex
characters of the SHA-256 of `main.dart.js`, so a phone fetches new code as soon as the code
changes. The step fails (non-zero exit) if `flutter_bootstrap.js` no longer has the
`"mainJsPath":"main.dart.js"` entry, e.g. after a Flutter upgrade.

- Cache-busted: `main.dart.js`.
- Not cache-busted: the `canvaskit/` engine files, fonts and images under `assets/`. They change
  only on a Flutter upgrade or an asset change and can be stale for up to 4 h through Cloudflare.
- Always fresh: the entry files `index.html`, `flutter_bootstrap.js`, `flutter_service_worker.js`,
  `version.json`, `manifest.json`.
- Staging build: `scripts/build-web.sh --base-href /web/` (all arguments go to `flutter build web`).

### Default server address (T-0022)

The compile-time define `DEFAULT_SERVER_URL` is prefilled in the server field when no server is
saved yet (default empty = `http://`, as on Android). Production web build:

```
scripts/build-web.sh --dart-define=DEFAULT_SERVER_URL=https://qoimeks.ai-dala.com
```
