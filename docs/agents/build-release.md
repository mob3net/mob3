# Build, test, release and the website

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- `make` = `mkdir -p bin && CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o bin/mob3 ./cmd/mob3`
- Dependencies are vendored in `vendor/`; `make vendor` regenerates it and the
  tree must build without network access. `vendor/` is committed, never cleaned.
- Build output lives in `bin/` (ignored by git); `make clean` removes `bin/`.
- `Dockerfile` is a multi-stage build: `golang:1.27-bookworm` cross-compiles
  the vendored static binary from `$BUILDPLATFORM`, and `alpine:3` runs it as a
  non-root user. Alpine is required so the image can cover
  `linux/arm/v6`; distroless has no armv6 manifest. The runtime image installs
  no CA certificate package: with no platform roots, Go's fallback trust uses
  the Mozilla NSS bundle compiled into the binary, so image trust matches the
  release instead of aging with the base image. Certificate files are not
  read, so private roots require a build with the bundle embedded. The build
  stage copies only
  `go.mod`, `go.sum`, `vendor/`, `cmd/` and `internal/`; do not use `COPY . .` or
  a `.dockerignore`. `make docker` builds `mob3`, and the image includes
  `LICENSE` and `THIRD_PARTY_NOTICES` under `/usr/share/doc/mob3`.
- `make test` = `go test -mod=vendor ./...`; tests must not require network
  access.
- Binaries and test/coverage output are covered by `.gitignore`; keep it in sync
  when the layout or build output changes.
- `make hooks` points `core.hooksPath` at `.githooks/` so the size-cap
  pre-commit check runs locally. The hook is optional per clone; the CI
  `Large files` step in `.github/workflows/ci.yml` runs
  `.githooks/check-large-files --all` and is the enforcement that cannot be
  skipped.
- `go vet ./...` must stay clean, and `gofmt` is applied to project files before
  handing work back. Do not reformat files under `vendor/`.
- Interactive mode is supported on Linux (`term_linux.go`) and macOS
  (`term_darwin.go`), sharing key decoding from `term_unix.go`; other platforms
  fall back to dump output. Windows is not a build target; Windows users run
  the Linux binary under WSL.
- Keep the binary out of layout decisions: it may be committed/present locally,
  but `make clean` removes it.
- The Git tag (`vX.Y.Z`) is the release version source of truth.
  `internal/browser.Version` defaults to `dev` and is set at build time with
  `-ldflags "-X browser/internal/browser.Version=<tag>"`; `mob3 -version` prints
  it, and `mob3 -h` prints usage plus the short build version, the
  `AGPL-3.0-only` license and the copyright notice. `-g` is an alias for
  `-graphics`; `about:config` shows the canonical `-graphics` name only.
  Release binaries and the Docker image both receive `github.ref_name`.
  Local builds use `make VERSION=vX.Y.Z` or
  `docker build --build-arg VERSION=vX.Y.Z`.
- `.github/workflows/ci.yml` runs gofmt, `go vet` and tests on pushes to `main`
  and on open, synchronize, reopen and edit pull-request events.
  `.github/workflows/pr.yml` runs only for pull requests and requires the exact
  statement `I have read and agree to the MOB³ Contributor License Agreement.`
  in the PR description.
- The GitHub Pages site lives on the generated `gh-pages` branch.
  `.github/pages/build.sh` converts `README.md` to `index.html` and every
  `docs/*.md` to `docs/*.html` with pinned `marked` (via `npx`), wraps each page
  in `.github/pages/template.html`, rewrites relative `.md` links to `.html`
  so the same Markdown works on GitHub and the site, generates the docs
  sidebar from the fixed `docs/` page order, and builds `search-index.js` for
  the client-side header search.
  `.github/workflows/pages.yml` rebuilds and force-pushes the branch on `main`
  pushes touching the README, `docs/`, page files or workflow, and on manual
  dispatch.
  **SEO and link previews** live in the same template and build script. Each
  page emits a canonical URL, a `<meta name="description">`, Open Graph and
  Twitter `summary_large_image` tags, a 1200×630 `og:image`, and JSON-LD
  (`WebSite` + free `SoftwareApplication` on the landing page,
  `BreadcrumbList` + `TechArticle` on docs pages). Per-page descriptions come
  from the `page_description` map in `build.sh`, falling back to the first prose
  paragraph; docs `<title>`s carry the `— MOB³ documentation` suffix. The build
  also writes `sitemap.xml`, `robots.txt` (pointing at the sitemap) and copies
  `.github/pages/assets/` to `assets/` plus `docs/screenshots/` to
  `docs/screenshots/`. Those assets are committed binaries — `favicon-32.png`,
  `favicon.ico`, `apple-touch-icon.png`, `icon-192.png`, `icon-512.png`,
  `og-image.png` (a 1200×630 card built from the README hero screenshot) and
  `site.webmanifest` — so the Pages build needs no image tooling. The icon is
  the MOB³ GitHub avatar (a black pixel robot) on a white background, padded
  and rounded for favicons and full-bleed white for the Apple/Android icons.
  Regenerate the PNGs if the avatar or the hero screenshot changes; do not
  hotlink GitHub, `interactive32.com` or attachment URLs from the page head.
  The header is a flexible bar: the brand alone on the left, then a right-hand
  group with Docs (`/docs/`), GitHub, Releases, Contact
  (`mailto:info@mob3.net`), the docs menu button, the compact search box and
  finally the theme toggle pinned to the far edge. The search sits immediately
  before the theme toggle and its results anchor under it. There is
  deliberately no "Back" link and no "Documentation" tagline in the header.
  Below 860px the search is hidden so the brand, all four links, the menu
  button and the theme toggle stay on one line; below 400px and 345px the link
  padding, font sizes and control sizes shrink in two steps so the row still
  fits a 320px terminal. The site theme follows the OS `prefers-color-scheme`
  until the visitor uses the toggle; the choice is stored in `localStorage`
  (`mob3-docs-theme`) for that browser only, and the `theme-color` meta tags
  follow the OS scheme.
  README screenshot galleries are a centered `<div align="center">` with each
  image wrapped in an `<a href="…" target="_blank" rel="noopener">` to its
  full-size asset, at `width="32%"`, with `<br>` row breaks. This renders three
  per row on GitHub without table borders (GitHub's table CSS adds borders and
  zebra rows) and makes the images clickable on both GitHub and the Pages site;
  the `target="_blank"` opens a new tab on mob3.net (GitHub strips `target`
  from user content, so its images open in the same tab). The template styles
  `.content div[align="center"] img` as inline 32%-wide blocks so the same
  markup lays out three per row on mob3.net. Screenshots are committed under
  `docs/screenshots/` and referenced by relative path; never hotlink GitHub
  `user-attachments` URLs in the README, because the redirect chain makes them
  slow to load. The README must also carry a
  visible link to `https://mob3.net` near the top (currently
  `Project page: [mob3.net](https://mob3.net).`).
  The build writes a `CNAME` for **`mob3.net`**; the site is served at
  `https://mob3.net` (apex A/AAAA DNS to GitHub Pages, `www` CNAME to
  `mob3net.github.io`, HTTPS enforced in the Pages settings). Never edit
  `gh-pages` by hand; change the README, template or build script.
- Releases are built by `.github/workflows/release.yml` on any `v*` tag push,
  not on `release: published`, because release-triggered workflows run from the
  default branch. The workflow checks gofmt, `go vet` and tests first, then
  builds seven targets with vendored dependencies and `CGO_ENABLED=0`: Linux
  386/amd64/armv6/armv7/arm64 and macOS amd64/arm64.
- Release assets are named `mob3_<tag>_<os>_<platform>.tar.gz` and include
  `SHA256SUMS`. Every archive contains `mob3`. The release job verifies that
  exactly seven archives were produced, creates the GitHub release from the tag
  with generated notes when it is missing, and uploads with `--clobber` so
  re-runs replace assets. The release job checks out full history so
  `gh release create --generate-notes` can find tags.
- `.github/workflows/docker.yml` runs after the `Release binaries` workflow
  completes successfully (`workflow_run`) and publishes a multi-arch image to
  `ghcr.io/mob3net/mob3` for `linux/386`, `linux/amd64`, `linux/arm64`,
  `linux/arm/v6` and `linux/arm/v7`, tagged with the release tag and `latest`.
  It resolves the tag from the triggering run and uses `GITHUB_TOKEN` with
  `packages: write`. The first GHCR package is private by default; making it
  public is a manual repository setting.
- Release jobs use native runners (Ubuntu, macOS) so signing tools are
  available. Signing is optional and driven by repository secrets; missing
  secrets publish unsigned archives rather than failing the release. macOS uses
  a Developer ID certificate plus `codesign` and optional `notarytool`
  notarization from `MACOS_CERT_P12_BASE64`, `MACOS_CERT_PASSWORD`,
  `MACOS_KEYCHAIN_PASSWORD`, `MACOS_SIGNING_IDENTITY` (first valid identity when
  unset), `MACOS_NOTARY_APPLE_ID`, `MACOS_NOTARY_TEAM_ID` and
  `MACOS_NOTARY_PASSWORD`.
