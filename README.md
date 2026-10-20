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
| `SEED_DEMO` | — | `1` loads a demo catalog into an empty database |
| `LLM_PROVIDER` | first provider whose key is set | who turns a spoken phrase into a draft operation, see below |
| `LLM_MODEL` | per provider | model used for that |
| `ELEVENLABS_API_KEY` | — | speech-to-text with ElevenLabs Scribe (`elevenlab_api_key` is accepted too) |
| `STT_API_KEY`, `STT_BASE_URL` | — | instead of ElevenLabs: any service with an OpenAI-style `/audio/transcriptions` |
| `STT_MODEL` | `scribe_v2` / `gpt-4o-transcribe` | speech model |

Settings can also be put in a `.env` file in the repository root or in `server/`.

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

## Download page for operators

`deploy/qoimeks-site/` is the static download page (ru/kk) of the Android app, published with
`scripts/publish-site.sh <target>`. Release and publish steps and the Caddy snippet are in
[docs/qoimeks-site.md](docs/qoimeks-site.md).

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
