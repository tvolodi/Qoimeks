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
   (default `../release-out`, outside git, the same for both scripts). It fails without `android/key.properties`.
2. Look at what would be uploaded:

   ```
   scripts/publish-site.sh --dry-run user@host:/srv/ai-dala/qoimeks/
   ```

3. Publish:

   ```
   scripts/publish-site.sh user@host:/srv/ai-dala/qoimeks/
   ```

   The destination is an `scp` directory (`$PUBLISH_CP` replaces `scp`). Order of the
   uploads: the template files, every `qoimeks-*.apk` with its checksum, and
   `latest.json` last, so the manifest never points at a missing file. Nothing is deleted
   on the server: older APKs stay available for rollback.
4. Open `https://ai-dala.com/qoimeks/` on a phone and check the version and the button.

Rollback: upload the previous `latest.json` (keep a copy of each release folder).

`bash scripts/test-publish.sh` checks the order and the absence of delete commands; it
also runs as part of `go test ./...` (package `internal/sitepublish`).

## nginx

`ai-dala.com` is the owner's Next.js site on `hetzner-prod`: the host's nginx terminates
TLS and proxies to the site on 127.0.0.1:3104. Cloudflare is in front of every domain
and caches by default, hence the `no-cache` headers below. Serve the download area from
a static host folder (here `/var/www/downloads/qoimeks/`, the `<target>` of the publish
script) with a `location` placed **before** the proxy `location /` in the vhost, so a
release never rebuilds the site:

```
location = /qoimeks { return 301 /qoimeks/; }

location /qoimeks/ {
    alias /var/www/downloads/qoimeks/;
    index index.html;

    types {
        text/html                                    html;
        text/css                                     css;
        application/javascript                       js;
        application/json                             json;
        application/vnd.android.package-archive      apk;
        text/plain                                   sha256;
    }

    # the page and the manifest must show a new release at once
    location ~ ^/qoimeks/(latest\.json|index\.html)?$ {
        alias /var/www/downloads/qoimeks/$1;
        add_header Cache-Control "no-cache";
    }

    # versioned APKs never change
    location ~ ^/qoimeks/(qoimeks-[^/]+\.apk)$ {
        alias /var/www/downloads/qoimeks/$1;
        add_header Cache-Control "public, max-age=31536000, immutable";
    }
}
```

Notes: a regex `location` with `alias` needs the capture (as above). For `/qoimeks/` the
capture is empty and `alias` resolves to the folder, which `index` turns into
`index.html`. `add_header` in a nested location replaces the parent's headers, so none are
set above. Reload with `nginx -t && systemctl reload nginx` (through the ai-dala-infra
project).

## The QR code

The QR code of `https://ai-dala.com/qoimeks/` is inline SVG in `index.html`, generated
once with the Go library `github.com/skip2/go-qrcode` (error correction level M), run
from a throwaway module outside the repository; one path per row of dark modules. No
runtime dependency. If the address changes, generate it again the same way.
