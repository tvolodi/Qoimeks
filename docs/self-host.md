# Self-host Qoimeks in 15 minutes

You need a Linux server with a public address, a domain name pointing at it, and
Docker with the Compose plugin. Nothing else is installed on the host except a reverse
proxy for HTTPS. For the settings of the server in detail see the
[README](../README.md#server-settings).

## 1. Get the code and start the stack

```sh
git clone <this repository> qoimeks && cd qoimeks/deploy
cp .env.example .env && chmod 600 .env
```

Edit `deploy/.env` (it is never committed):

- `POSTGRES_PASSWORD` — letters and digits only, it is placed inside a database URL;
- `ACCESS_TOKEN` — the shared access code of all operators, for example `openssl rand -hex 32`;
- `HOST_PORT` — the loopback port for the proxy (default `3113`).

The speech and language-model keys are optional, see step 3. Then start the stack
(the server image is built from `server/Dockerfile`, PostgreSQL gets its own volume):

```sh
docker compose -f docker-compose.prod.yml up -d --build
curl -fsS http://127.0.0.1:3113/healthz      # use your HOST_PORT
```

`/healthz` answers `ok` when the server is up. The port
is published on `127.0.0.1` only, so the server is not yet reachable from outside:
that is the next step. Later updates: `./redeploy-prod.sh` (it pulls `main`, rebuilds,
restarts and waits for `/healthz`).

## 2. HTTPS reverse proxy

The phone app and the browser must reach the server over HTTPS. Any proxy works; the two
smallest examples (replace `qoimeks.example.com` and the port):

Caddy (gets a certificate by itself), in `/etc/caddy/Caddyfile`:

```
qoimeks.example.com {
    reverse_proxy 127.0.0.1:3113
}
```

nginx (certificate from `certbot --nginx -d qoimeks.example.com`):

```
server {
    listen 443 ssl;
    server_name qoimeks.example.com;
    # ssl_certificate lines are added by certbot

    client_max_body_size 20m;       # workbooks up to 12 MB and voice recordings

    location / {
        proxy_pass http://127.0.0.1:3113;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Open `https://qoimeks.example.com/healthz` from another machine to check.

## 3. Environment variables

Everything is set in `deploy/.env`; the full list with defaults is in the README
([Server settings](../README.md#server-settings)). The ones you need first:

| Variable | What for |
|---|---|
| `ACCESS_TOKEN` | the shared access code; leave it empty and anyone who can reach the server can use it |
| `ELEVENLABS_API_KEY` | speech to text (or `STT_API_KEY` and `STT_BASE_URL` for an OpenAI-style service); without it the app works from the keyboard |
| `ANTHROPIC_API_KEY` | language model that turns a spoken phrase into a draft; or one of `OPENAI_API_KEY`, `XAI_API_KEY`, `DEEPSEEK_API_KEY`, `MINIMAX_API_KEY` |
| `LLM_PROVIDER`, `LLM_MODEL` | optional: choose the provider and the model explicitly |

Both providers are optional: with no keys the server still stores and shows stock, and
the operators fill the form by hand. After editing `.env` apply it with
`docker compose -f docker-compose.prod.yml up -d --force-recreate app`.

## 4. First catalog import

1. Open `https://qoimeks.example.com/` in a browser and press the button at the top right.
   Enter the `ACCESS_TOKEN` and your name.
2. Go to the import page, choose the bookkeepers' `.xlsx` workbook (article, name,
   closing balance, zone, rack, shelf, second place) and press **«Проверить»**. Nothing changes yet:
   the page shows what would be loaded.
3. Tick **Записать остатки из файла** to also take the balances from the file (on an empty
   database that is what you want), then press **«Загрузить в систему»**. The file is read in memory and not
   stored. A wrong load can be taken back with **Отменить** in the import history.

An export of the current stock in the same layout is on the same page, so a file can be
corrected in Excel and loaded again. The README section
[Stock overview and Excel in the browser](../README.md#stock-overview-and-excel-in-the-browser)
describes the format and the rules.

## 5. Connect the app

Install the app on the phones: the Android steps (allow unknown apps, download, update) are
on the download page of the project, see [docs/qoimeks-site.md](qoimeks-site.md) to host that
page yourself, or build the app from `mobile/` with `flutter build apk`.

Then, the easy way:

1. In the web interface, after entering the access code, find the block
   **«Подключить приложение»**: a QR code with the server address and the code.
2. In the app open Settings, press **«Сканировать QR / QR сканерлеу»**, scan, then press
   **«Сохранить»** and enter the operator's name.

Whoever photographs that QR can use the server, so do not show it to others. By hand:
in Settings type the server address (`https://qoimeks.example.com`), the access code and the
name. Post a test operation, then see it in the web balances and in the app's History.

## Backups and updates

Everything is in the `dbdata` Docker volume of the `db` service (PostgreSQL 16): back it up with
`docker exec qoimeks-db pg_dump -U qoimeks qoimeks > qoimeks.sql`. The ledger tables reject
UPDATE and DELETE; mistakes are cancelled by new movements.
