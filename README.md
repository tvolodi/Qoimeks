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

## Loading the catalog from Excel

The bookkeepers' weekly workbook (article, name, closing balance, zone, rack, shelf,
second place) is loaded with:

```sh
cd server
go run ./cmd/import -dry-run path/to/stock.xlsx    # read and report only
go run ./cmd/import -balances path/to/stock.xlsx   # save catalog and opening balances
```

The first sheet (the newest week) is read unless `-sheet` names another. A place is
written as zone + rack + shelf, e.g. `RI2`. Running the import again updates names and
places and adds new articles; `-balances` only records stock for an article and place
the ledger has never seen, so balances operators already work with are not overwritten.
Rows the importer cannot trust are listed and skipped, never guessed.

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
