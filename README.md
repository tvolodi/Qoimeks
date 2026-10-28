# Qoimeks

Stock accounting by SKU and location for warehouse operators. The operator fills one
form — by hand or by saying the operation aloud in Russian or Kazakh — checks it, and
posts it.

- `server/` — Go API and PostgreSQL ledger
- `mobile/` — Flutter app (Android, iOS, web)

## Run it

```sh
docker compose up -d --wait          # PostgreSQL on 127.0.0.1:5433

cd server
SEED_DEMO=1 go run ./cmd/server      # http://localhost:8080; SEED_DEMO loads a demo catalog into an empty database

cd ../mobile
flutter run -d chrome                # or a connected phone
```

On first launch the app asks for the operator's name and the server address
(`http://<server-ip>:8080`; from the Android emulator use `http://10.0.2.2:8080`).

## Server settings

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | `postgres://qoimeks:qoimeks@127.0.0.1:5433/qoimeks` | PostgreSQL |
| `LISTEN_ADDR` | `:8080` | HTTP address |
| `ACCESS_TOKEN` | — | shared access code every app must send; without it anyone who can reach the server can use it |
| `PROVISIONING_TOKEN` | — | bearer token of the tenant API (`/admin/v1`) used by the ai-dala.com site; without it the API is off (404). Use 32+ random characters |
| `PUBLIC_URL` | `https://qoimeks.ai-dala.com` | server address returned to the site as a tenant's `server_url` |
| `SEED_DEMO` | — | `1` loads a demo catalog into an empty database |
| `LLM_PROVIDER` | first provider whose key is set | who turns a spoken phrase into a draft operation, see below |
| `LLM_MODEL` | per provider | model used for that |
| `ELEVENLABS_API_KEY` | — | speech-to-text with ElevenLabs Scribe (`elevenlab_api_key` is accepted too) |
| `STT_API_KEY`, `STT_BASE_URL` | — | instead of ElevenLabs: any service with an OpenAI-style `/audio/transcriptions` |
| `STT_MODEL` | `scribe_v2` / `gpt-4o-transcribe` | speech model |
| `SETTINGS_KEY` | — | base64 of 32 random bytes (`openssl rand -base64 32`); encrypts the provider API keys stored on the server. Without it stored keys are not used and cannot be saved |

Settings can also be put in a `.env` file in the repository root or in `server/`.

Provider settings stored in the database (language model and speech: provider, model, API key) win
over the variables above; the environment variables (`LLM_*`, the `*_API_KEY` ones, `STT_*`,
`ELEVENLABS_API_KEY`) are the fallback when nothing is stored. The fallback also applies when a
stored key cannot be decrypted because `SETTINGS_KEY` is missing or was changed: the server then
uses the environment and logs a warning without any key material. Changes apply to the next
request, no restart. Stored keys are server-wide (not per tenant), shown only masked and never
sent to the app. Keep `SETTINGS_KEY` safe: if it is lost, stored keys cannot be recovered and
must be entered again.

### Language model

| `LLM_PROVIDER` | Key variable | Default model |
|---|---|---|
| `anthropic` | `ANTHROPIC_API_KEY` | `claude-opus-5-5` |
| `openai` | `OPENAI_API_KEY` | `gpt-4o-mini` |
| `xai` | `XAI_API_KEY` | `grok-4.20-0309-non-reasoning` |
| `deepseek` | `DEEPSEEK_API_KEY` | `deepseek-v4-flash` |
| `minimax` | `MINIMAX_API_KEY` | `MiniMax-M2.5` |
| `custom` | `LLM_API_KEY` | none, set `LLM_MODEL` and `LLM_BASE_URL` |

`custom` is any other service with an OpenAI-style `/chat/completions`. `LLM_API_KEY`,
`LLM_BASE_URL` and `LLM_MODEL` override the defaults of any provider. `LLM_JSON_MODE`
(`schema`, `object` or `prompt`) says how strictly the service can be asked for JSON;
use `prompt` if a service rejects the request. Model names change often: if a default
is refused, set `LLM_MODEL` to a current one. An API key is not the same as a ChatGPT
or Claude subscription; it is created and billed separately on the provider's developer site.

Without a speech key the app works from the keyboard only. With a speech key but no
language model, the microphone shows what was heard and the operator fills the form by hand.

### Feedback

`POST /api/feedback` (bearer access code, like the other `/api` routes) appends a report from
the app to the append-only `feedback` table of the caller's schema (the tenant's own, or the
default one) and answers `201 {"id": N}`. JSON body: `text` (required, 1–4000 characters),
`voice_text` (optional, up to 4000, a transcribed voice note), `app_version`, `android_version`,
`device`, `operator`, `screen`, `provider` (each up to 200 characters) and `errors` (an array of
at most 50 entries, strings or objects). Too long a text, more than 50 errors or a body that is
not JSON gives 400. Rows get `kind` `user`; the table also has room for server-recorded kinds
(`speech_error`, `llm_error`, `post_error`). UPDATE, DELETE and TRUNCATE on it are rejected by
the database. Before storing, every string in the request has the server `ACCESS_TOKEN`, the
code the request was sent with (a tenant's code), every configured provider key of 8+ characters,
`Bearer <token>` and `sk-…` keys replaced with `[redacted]`.

The server also records failures nobody reported, in the caller's own schema: a failed speech
call in `/api/voice` (`speech_error`), a failed language-model call in `/api/parse` and
`/api/voice` (`llm_error`) and a server-side (5xx) failure of `POST /api/movements`
(`post_error`); validation errors (4xx) are not recorded. The row has the provider name, the
error class and message (redacted as above) and, when the app sent them, `X-App-Version`,
`X-Device` and `X-Operator` (percent-encoded; stored in `app_version`, `device`, `operator`).
At most one row per schema, kind and provider is appended per minute (in-memory window; rows
are never updated), so a broken provider key shows up as one row a minute, not one per call.

`GET /api/feedback` (bearer access code) lists the stored items newest first as JSON, up to
`?limit=` rows (default 200, at most 1000); `GET /api/feedback/export.md` returns the newest
1000 as a Markdown file with text, kind, version, Android, device, operator, screen, time and
errors of every item, for handing to the team. The web interface has a section «Отзывы»
(`/#feedback`, after signing in): items newest first with all details, a table of kinds with the
count and last time of each (click a row to filter), a «новое» mark on items newer than the
newest id seen on the previous visit (kept in the browser's localStorage), and a Markdown download.

## Stock overview and Excel in the browser

The server's root page (`http://<server>:8080/`) is the web interface. The button at the top
right asks for the access code (`ACCESS_TOKEN`) and the person's name; everyone with the code
can use all of it. The code is kept until the browser is closed, the name is remembered. The
interface has three pages: balances, import and export, and the import history.

- **Balances** by SKU (with the places each is held in) or by place (with the SKUs on it), with search.
- **Выгрузить в Excel** downloads the current stock as a workbook in the layout the importer
  reads, one row per SKU and place, so a file can be corrected in Excel and loaded back.
- **Загрузка из Excel** loads the bookkeepers' weekly workbook (article, name, closing balance,
  zone, rack, shelf, second place) in two steps: **check** reads the file and shows what would
  change without changing anything, **load** applies exactly the file that was checked. The file
  is read in memory and never stored; who loaded what and when is kept in the history.

The first sheet (the newest week) is read unless another is chosen. A place is written as
zone + rack + shelf, e.g. `RI2`. Loading again updates names and places and adds new
articles.

The bookkeepers' file is the authority on balances. With **Записать остатки из файла** every
balance that differs from the file is replaced by the file's quantity through a stock-count
movement (`adjustment`, source `import`), so the history keeps each difference. An article of
the file is emptied at places the file does not list for it; articles the file does not
mention are left alone and listed. The check shows all of this before anything changes.

Every movement an import posts carries the import's number, so a wrong load can be taken back
as a whole: **Отменить** in the history reverses each of its balance changes. Nothing is
deleted — the ledger is append-only — and movements posted after the import stay. Names and
places the import wrote into the catalog are not reverted.

A balance typed as text is read whatever its decimal sign: `1,5`, `1.5`, `1 250,5`, `1.250,75`
and `1,250.75` all work, and a single comma or dot is always the decimal sign (`1,000` is one,
as 1C writes it). A balance that is not a number leaves that article's balances untouched.

An article is not unique in the file: its rows are added up by place. Rows that repeat an
article under another name, or repeat the same place, are loaded and remarked on.

The same import exists as a command for emergencies:

```sh
cd server
go run ./cmd/import -dry-run path/to/stock.xlsx    # read and report only
go run ./cmd/import -balances path/to/stock.xlsx   # save catalog and replace balances
```

## Access

There are no user accounts. The server has one shared access code (`ACCESS_TOKEN`), and each
operator enters it once in the app's settings together with their name. The name is what
appears in the history; the code only decides who may use the server at all. To cut off
access, change the code on the server and give the new one to the operators.

### Connecting the app by QR

The web page has a "Подключить приложение" block with a QR code that the app can scan instead
of typing the server address and code. The QR is drawn in the browser (no external resources,
no extra server endpoint); the server never receives the code for this.

Payload:

```
qoimeks://setup?url=<urlencoded server address>[&code=<urlencoded access code>]
```

- `url` is always present: the page's own address without a trailing slash (`location.origin`).
- `code` is added only after the code has been entered and accepted in that browser, and
  disappears on sign-out. Before that the QR holds the address only.
- Both values are percent-encoded as UTF-8 (JavaScript `encodeURIComponent`: letters, digits and
  `-_.!~*'()` stay, everything else becomes `%XX`). The order is `url`, then `code`. A reader
  ignores parameters it does not know.
- Example: `qoimeks://setup?url=https%3A%2F%2Fqoimeks.ai-dala.com&code=s3cret%20code`

A QR that contains the code lets anyone who photographs it use the server: do not show it to
others. The page says so next to the QR. The QR is a version 1–10 code at error-correction
level M, so the address plus a code of about 60 characters fits.

The app's settings have a "Сканировать QR / QR сканерлеу" button that opens the camera and
reads this QR. The scan fills the server address and, when present, the access code field; the
operator still presses "Сохранить", and a payload without a code leaves the code field as it is.
The code is never shown in a message or logged. Any other QR code, a denied camera permission or
a phone without a camera shows a message in the app language and changes nothing; typing the
values by hand always works. The app asks for the Android `CAMERA` permission only when the
button is pressed.

## Tenants

One server can host several tenants. Each tenant is a PostgreSQL schema (`t_<tenant id>`) with its
own copy of the ledger tables and the same append-only triggers; the default schema is the
original single-tenant install and keeps working with `ACCESS_TOKEN`. A tenant has its own
access code: the app and the web interface send it like `ACCESS_TOKEN`, and the server serves
the request from that tenant's schema. A suspended tenant gets 403 `tenant suspended`; its data is kept. A demo tenant with `expires_at` in the past, or one expired through `POST /admin/v1/tenants/{id}/expire`, gets 403 `tenant expired` (data kept).

The site creates, suspends and resumes tenants through `/admin/v1` (bearer `PROVISIONING_TOKEN`,
30 requests a minute); the contract is in `docs/public-page-and-signup-draft.md`, section 3.

Notes for developers:
- With an empty `ACCESS_TOKEN` the default schema stays open to any request, as before; a tenant
  is chosen only by an exact match of its access code.
- A new migration runs in every tenant schema (at server start and when a tenant is first used)
  unless it is listed in `publicOnly` in `server/internal/store/migrate.go`.
- Each active tenant keeps a pool of up to 4 connections; size PostgreSQL `max_connections` for that.
- The posting lock in `appendBalance` is database-wide, so tenants with equal numeric ids may
  briefly wait for each other. That is intended.

## Production

`deploy/` holds the production stack: the server and its own PostgreSQL in Docker Compose,
published on a loopback port for a reverse proxy that adds HTTPS.

```sh
cd deploy
cp .env.example .env && chmod 600 .env    # fill in the values
./redeploy-prod.sh                        # pull, build, restart, wait for /healthz
```

The catalog is then loaded from `https://<host>/`.

## Release build

`scripts/release-android.sh` builds the signed release APK and writes the files to publish
into an output folder outside git (default `../release-out`):

```sh
scripts/release-android.sh --first-release --notes-ru "Что нового" --notes-kk "Жаңалықтар"
scripts/release-android.sh --notes-file notes.txt     # lines "ru: ..." and "kk: ..."
scripts/release-android.sh --dry-run ...              # run the checks, print the build command
```

The output folder gets `qoimeks-<version>.apk`, `qoimeks-<version>.apk.sha256` and
`latest.json` (`versionCode`, `versionName`, `url`, `sha256`, `minVersionCode`, `notes.ru`,
`notes.kk`, `published`). Version name and code come from `mobile/pubspec.yaml`. The script
fails when `mobile/android/key.properties` is missing (a release is never signed with the
debug key), and when the version code is not higher than the last published one, read from
`<out>/latest.json` or `--last-published N`; with neither, pass `--first-release`.
Other options: `--out DIR`, `--base-url URL` (default `https://ai-dala.com/qoimeks/`),
`--min-version-code N` (default: the previous manifest value). The script test is
`bash scripts/test-release.sh`; `go test ./...` runs it too.

### Fonts

The app bundles the variable fonts Onest and JetBrains Mono in `mobile/assets/fonts/`
(SIL Open Font License 1.1; the licence texts `OFL-Onest.txt` and `OFL-JetBrainsMono.txt` are
kept next to the files). Nothing is fetched at build or run time, and the golden test loads
the same files, so its images look the same on any machine. The web pages keep system fonts.

## Download page for operators

`deploy/qoimeks-site/` is the static download page (ru/kk) of the Android app, published with
`scripts/publish-site.sh <target>`. Release and publish steps and the nginx snippet are in
[docs/qoimeks-site.md](docs/qoimeks-site.md).

## Release build

`scripts/release-android.sh` builds the signed release APK and writes the files to publish
into an output folder outside git (default `../release-out`):

```sh
scripts/release-android.sh --first-release --notes-ru "Что нового" --notes-kk "Жаңалықтар"
scripts/release-android.sh --notes-file notes.txt     # lines "ru: ..." and "kk: ..."
scripts/release-android.sh --dry-run ...              # run the checks, print the build command
```

The output folder gets `qoimeks-<version>.apk`, `qoimeks-<version>.apk.sha256` and
`latest.json` (`versionCode`, `versionName`, `url`, `sha256`, `minVersionCode`, `notes.ru`,
`notes.kk`, `published`). Version name and code come from `mobile/pubspec.yaml`. The script
fails when `mobile/android/key.properties` is missing (a release is never signed with the
debug key), and when the version code is not higher than the last published one, read from
`<out>/latest.json` or `--last-published N`; with neither, pass `--first-release`.
Other options: `--out DIR`, `--base-url URL` (default `https://ai-dala.com/qoimeks/`),
`--min-version-code N` (default: the previous manifest value). The script test is
`bash scripts/test-release.sh`; `go test ./...` runs it too.

### Update check in the app

The release build passes `--dart-define=UPDATE_MANIFEST_URL=<base-url>latest.json` to
`flutter build` (default `https://ai-dala.com/qoimeks/latest.json`; `--base-url` changes the
base, `--manifest-url URL` sets the whole URL, `--no-update-check` builds with the setting
empty). With an empty `UPDATE_MANIFEST_URL` (also a plain `flutter run`, or a future Google
Play build, where self-updates are forbidden) the app never checks and Settings has no
"check for updates" button.

With a URL, the app reads the manifest on start, at most once a day. A newer `versionCode`
shows a banner with the notes in the app language; the operator can dismiss it. An installed
code below `minVersionCode` shows a banner that cannot be dismissed. Tapping the banner opens
the manifest's `url` in the browser (the app has no install permission). Settings show the
installed version and a "check for updates" button that checks at once. A missing network or a
broken manifest shows nothing on the automatic check and never blocks work.

### Report a problem from the app

The main screen (feedback icon) and Settings have «Сообщить о проблеме / Пікір жіберу»: a form
with free text. It posts to `POST /api/feedback` with the text, app version, Android version,
device model, operator name, the screen it was sent from and the app's last 50 errors
(`voice_text` is empty until voice notes exist). The errors are kept on the phone in shared
preferences (time, screen, message); the access code and bearer tokens are never stored — the
saved code is replaced by `[redacted]`. When the server cannot be reached the report is kept
on the phone, the form says so, and it is sent on the next app start or after the next
successful API call, once.

## How data is stored

Two tables, both append-only (the database rejects `UPDATE` and `DELETE`):

- `movements` — one row per operation: receipt, issue, transfer, or adjustment (stock count).
- `balances` — one row per movement and location it touched: the change and the balance
  after it. The current balance is the latest row for a SKU and location.

A mistake is cancelled from the History screen, which appends a reversing movement.
Each operation carries an ID generated on the phone, so sending it twice cannot post it twice.
Issuing more than the balance is allowed; the app warns and the balance goes negative.

## Tests

```sh
cd server && TEST_DATABASE_URL=postgres://qoimeks:qoimeks@127.0.0.1:5433/qoimeks go test ./...
cd mobile && flutter test
```

Server tests create and drop their own database.

## Licence

Qoimeks is free software under the GNU Affero General Public License v3.0, see
[LICENSE](LICENSE). You may run, change and host it; if you offer a changed version to
others over a network, you must publish your changes under the same licence.
