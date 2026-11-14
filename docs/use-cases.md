# Use cases

MOB³ is a terminal browser with a headless MCP mode, so it fits anywhere a
graphical browser does not: SSH sessions, containers, CI runners, remote
servers and AI agents. It is small and text-focused, not a full browser engine,
so the best results come from content pages, docs and server-rendered HTML; see
[what to expect](https://github.com/mob3net/mob3#what-to-expect) and
[site support](sites.md).

Remember that MOB³ is **not a secure browser**: no sandboxing, no site
isolation, no CSP. Use it for reading and automation, never for passwords,
accounts, payments or confidential data. See [security](security.md).

## Developers and sysadmins

- **Terminal browsing** — browse websites directly from SSH sessions, servers,
  containers and minimal Linux environments.
- **Headless website testing** — load pages, execute JavaScript, inspect
  rendered content and verify website behavior without Chromium.
- **Web scraping** — extract text, links, metadata and other content from
  JavaScript-driven pages.
- **Website debugging** — inspect how HTML, CSS and JavaScript render without
  opening a graphical browser.
- **Screenshot generation** — capture web pages as PNG images directly from
  scripts or the command line (`mob3 -png ... > page.png`).
- **CI/CD integration** — include lightweight browser checks in automated
  pipelines.
- **Server-side automation** — automate web interactions on remote machines
  without a desktop environment.

## AI agents and MCP

Start with `mob3 -mcp` and the [automation reference](automation.md).

- **AI web browsing** — give AI agents the ability to browse and interact with
  real websites through MCP.
- **AI research assistants** — let agents navigate websites, follow links,
  collect information and summarize findings.
- **Autonomous web automation** — allow agents to fill out forms, click
  buttons, navigate pages and execute workflows.
- **Local AI browser** — provide a browser that runs locally, without sending
  browser activity through a third-party automation service.
- **Agentic workflows** — integrate web browsing into multi-step AI workflows
  and scripts.
- **Website inspection by AI** — let agents inspect rendered pages, discover
  interactive elements and understand page structure.

## Everyday terminal users

- **SSH browsing** — browse the web from remote servers without installing a
  desktop environment.
- **Distraction-free browsing** — access websites without the overhead of a
  graphical browser.
- **Low-resource environments** — browse on machines with limited memory, CPU
  or graphical capabilities.
- **Privacy-conscious browsing** — keep browsing history, cookies, storage and
  caches in memory only; nothing is written to disk.
- **Inline image viewing** — view web images directly in compatible terminals
  with `-graphics=auto|kitty|iterm|blocks`.

## Automation and integration

- **Shell scripting** — fetch and interact with websites as part of shell
  scripts, for example with `mob3 -dump URL`.
- **Monitoring** — periodically inspect web pages and detect content changes.
- **Documentation lookup** — browse online documentation directly from
  development environments.
- **Link checking** — visit URLs and inspect whether pages render correctly.
- **Content conversion** — turn rendered web pages into text or screenshots for
  further processing.
- **Custom tooling** — build other applications and agents on top of MOB³'s
  browser capabilities.
