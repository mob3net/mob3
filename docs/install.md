# Install

## Release archives

Download the latest archive from
[GitHub Releases](https://github.com/mob3net/mob3/releases/latest). Each
release contains:

- `mob3_<tag>_linux_386.tar.gz`
- `mob3_<tag>_linux_amd64.tar.gz`
- `mob3_<tag>_linux_armv6.tar.gz`
- `mob3_<tag>_linux_armv7.tar.gz`
- `mob3_<tag>_linux_arm64.tar.gz`
- `mob3_<tag>_darwin_amd64.tar.gz`
- `mob3_<tag>_darwin_arm64.tar.gz`
- `SHA256SUMS`

Every archive contains the static `mob3` binary. Example:

```sh
tar -xzf mob3_vX.Y.Z_linux_amd64.tar.gz
sudo install -m 0755 mob3 /usr/local/bin/mob3
mob3 -version
```

Windows users run the Linux binary under WSL. Interactive mode needs a real
Linux or macOS terminal.

## Docker

```sh
docker run --rm -it ghcr.io/mob3net/mob3 -graphics blocks
```

The image is published for `linux/386`, `linux/amd64`, `linux/arm64`,
`linux/arm/v6` and `linux/arm/v7`. It runs as a non-root user and includes CA
certificates, `LICENSE` and third-party notices. It is a terminal application:
use `-it` to attach it to a TTY.

## MCP clients

After installing `mob3`, you can use it as a headless MCP server. OpenCode,
Claude Code, Cursor and Claude Desktop all start it as a local stdio server:

```sh
mob3 -mcp -width 100 about:home
```

For OpenCode, one command registers it globally:

```sh
opencode mcp add mob3 --global -- /usr/local/bin/mob3 -mcp -width 100 about:home
```

See [Automation with MCP](automation.md#client-configuration) for the OpenCode,
Claude Code, Cursor and Claude Desktop configuration details.

## Build from source

Requirements:

- Go 1.27 or newer
- `make`
- no C toolchain: the build is `CGO_ENABLED=0` and produces a static binary
- dependencies are vendored, so no network is needed to build

```sh
make
./bin/mob3 https://example.com
```

Useful targets:

```sh
make            # static binary in bin/
make test       # go test -mod=vendor ./...
make vendor     # refresh vendor/ after dependency changes
make docker     # local container image
make clean      # remove bin/
```

Set a release version locally:

```sh
make VERSION=vX.Y.Z
./bin/mob3 -version
```

`internal/browser.Version` defaults to `dev`; release builds inject the Git tag
through linker flags. The built binary is ignored by Git and `make clean`
removes it.

## First run

No configuration is written. The first run opens `about:home` unless a URL or
`file:` path is supplied. Use `-home URL` to choose a different start page.
