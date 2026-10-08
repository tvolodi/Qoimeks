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
SEED_DEMO=1 go run ./cmd/server      # http://localhost:8080, demo catalog on first start

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
| `ANTHROPIC_API_KEY` | — | turns a spoken phrase into a draft operation |
| `LLM_MODEL` | `claude-opus-5-5` | model used for that |
| `STT_API_KEY` | — | speech-to-text service key |
| `STT_BASE_URL` | `https://api.openai.com/v1` | any service with an OpenAI-style `/audio/transcriptions` |
| `STT_MODEL` | `gpt-4o-transcribe` | speech model |

Without the two keys the app still works from the keyboard; the microphone button
reports that voice input is not configured.

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
