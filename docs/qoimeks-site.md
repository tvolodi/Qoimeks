# Download site: owner runbook

The operators install the app from `https://ai-dala.com/qoimeks/`. The site is static:
the template in `deploy/qoimeks-site/` (`index.html`, `site.css`, `site.js`), plus the
files of a release (`qoimeks-<version>.apk`, its `.sha256`, `latest.json`).

The page reads `./latest.json` when it loads and fills in the version, the release notes
(ru and kk) and the download button. If the manifest cannot be read, the page keeps
working: it shows a message with a link to `latest.json` and tells the operator to ask
the owner, and the install steps (which are plain HTML) stay readable without JavaScript.
The page has no external resources: no CDN, no libraries.

## Release, then publish

1. Build the release: run the release script (see the release docs). It writes
   `qoimeks-<version>.apk`, its SHA-256 and `latest.json` into the output folder
   (default `./release-out`, outside git). It fails without `android/key.properties`.
2. Look at what would be uploaded:

   ```
   scripts/publish-site.sh --dry-run --out release-out user@host:/srv/ai-dala/qoimeks/
   ```

3. Publish:

   ```
   scripts/publish-site.sh --out release-out user@host:/srv/ai-dala/qoimeks/
   ```

   The destination is an `scp` directory (`$PUBLISH_CP` replaces `scp`). Order of the
   uploads: the template files, every `qoimeks-*.apk` with its checksum, and
   `latest.json` last, so the manifest never points at a missing file. Nothing is deleted
   on the server: older APKs stay available for rollback.
4. Open `https://ai-dala.com/qoimeks/` on a phone and check the version and the button.

Rollback: upload the previous `latest.json` (keep a copy of each release folder).

`bash scripts/test-publish.sh` checks the order and the absence of delete commands; it
also runs as part of `go test ./...` (package `internal/sitepublish`).

## Caddy

Serve the folder as static files. `.apk` gets the Android package type and is downloaded,
`latest.json` is never cached (so an update shows at once):

```
ai-dala.com {
    handle_path /qoimeks/* {
        root * /srv/ai-dala/qoimeks
        @apk path *.apk
        header @apk Content-Type application/vnd.android.package-archive
        header @apk Content-Disposition attachment
        @manifest path /latest.json
        header @manifest Cache-Control "no-cache, no-store, must-revalidate"
        header @manifest Content-Type application/json
        file_server
    }
    redir /qoimeks /qoimeks/
}
```

(Put the `handle_path` block next to the other routes of the site. `handle_path` strips
the `/qoimeks` prefix, so the paths in the matchers are relative to the folder.)

## The QR code

The QR code of `https://ai-dala.com/qoimeks/` is inline SVG in `index.html`, generated
once with the Go library `github.com/skip2/go-qrcode` (error correction level M), run
from a throwaway module outside the repository; one path per row of dark modules. No
runtime dependency. If the address changes, generate it again the same way.
