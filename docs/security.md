# Security and privacy

> **Warning:** **MOB³ is not a secure browser.** It has no sandboxing, process
> or site isolation, CSP enforcement, certificate UI, phishing protection or
> security audit. Treat every page as code running beside the browser. Do not
> use MOB³ for passwords, accounts, payments or confidential data.

## What it does not do

- No sandbox, process isolation, site isolation or permission prompts.
- No CSP enforcement, certificate warnings or certificate management UI.
- No phishing, malware, tracker or fingerprinting protection.
- No proxy configuration UI.
- No private/incognito concept: every session is ephemeral, but pages and
  scripts are not isolated from each other inside the process.

## No persistence

The running browser does not create or update configuration, history, cookie,
cache, log or download files. Closing MOB³ saves nothing.

- History, cookies, `localStorage`/`sessionStorage`, the image cache and all
  JavaScript state live in memory only.
- The download popup only shows a `curl` command; it writes nothing.
- Disk reads are limited to explicit local `file:` pages and their resources,
  an explicitly supplied `-bookmarks` file, and `/proc/self/statm` for the
  Linux memory footer.
- OS-level reads by Go's TLS, DNS and timezone libraries are not application
  storage.

## Cookies and storage

Cookies are session-only and shared by page loads, resources, XHR, `fetch` and
`document.cookie`. HttpOnly cookies are hidden from JavaScript. `SameSite` is
stored but not enforced. `localStorage` and `sessionStorage` are in-memory maps
and do not persist across restarts.

## Cross-origin requests

Pages can load cross-origin scripts, styles and images like a normal browser,
and share the cookie jar for those requests. JavaScript `fetch` and
`XMLHttpRequest` allow same-origin requests and only simple CORS cross-origin
requests with a matching `Access-Control-Allow-Origin`; preflight and
credentialed cross-origin requests are unsupported.

## Blocked sites and challenges

On 401/403/429/503 responses, MOB³ looks for common anti-bot/CAPTCHA signatures
and shows a generic “Site blocked” page with the reason, status and URL. The
site's challenge scripts are not executed unless you follow the one-shot
**Try anyway** link, or start with `-ignore-blocks`.

Challenge pages can run arbitrary script after that bypass. MOB³ cannot
complete most browser checks, and it is not a security boundary.

## Local files and downloads

Remote pages cannot navigate the viewport to `file:` URLs or load local file
resources. Local documents can load local stylesheets, scripts and images.
Only regular files are read; devices and FIFOs are rejected.

MOB³ never saves downloads. A download link or non-HTML response opens a popup
with a ready-to-copy `curl` command. Running that command is your choice and
outside MOB³.

## Automation

`-mcp` runs a local, unauthenticated automation server over stdin/stdout. Any
process connected to that stdin/stdout can navigate, click, type and evaluate
JavaScript in the page. Do not expose the MCP server as a network service and
do not connect untrusted clients. Sessions and storage remain memory-only.

## JavaScript safety limits

Each script and callback runs under a 2-second goja interrupt watchdog and a
configurable heap budget (`-js-memory`, default 256 MB). Timer counts and
per-tick callbacks are capped. These limits protect against runaway scripts,
not against malicious pages in general.
