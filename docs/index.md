# MOB³ documentation

MOB³ is a small web browser for the terminal, written in Go. It has its own
HTML parser, CSS cascade, layout engine, ANSI renderer and pure-Go JavaScript
runtime. History, cookies, storage and caches live in memory only.

> **Warning:** MOB³ is **not a secure browser**. It has no sandboxing, process
> or site isolation, CSP enforcement, certificate UI or security audit. Do not
> use it for passwords, accounts, payments or confidential data.

## Start here

| Page | What it covers |
| --- | --- |
| [Install](install.md) | Releases, Docker, build from source |
| [Command line](cli.md) | Every flag, defaults, examples and limits |
| [Keyboard and mouse](keys.md) | Navigation, prompts, find, console and forms |
| [Automation with MCP](automation.md) | Headless browser control for scripts and AI tools |
| [Security and privacy](security.md) | What MOB³ does and does not protect |
| [Site support](sites.md) | What renders well and what usually fails |
| [Development](development.md) | Build, test, architecture and contributing |

## Quick start

Download an archive from
[GitHub Releases](https://github.com/mob3net/mob3/releases/latest), or run the
container:

```sh
docker run --rm -it ghcr.io/mob3net/mob3 -graphics blocks
```

Build from source:

```sh
make
./bin/mob3 https://example.com
```

Press `?` inside the app for the built-in help page, and run `mob3 -h` for the
full flag list.

Project page: [mob3.net](https://mob3.net).
