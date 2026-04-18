# tavily-cli

Thin Go CLI wrapping the [Tavily](https://tavily.com) search API. Emits JSON on
stdout, progress on stderr. Agent-friendly.

Binary name: `tavily`.

## Install

**Homebrew (macOS)** — recommended on Mac:

```sh
brew install sapihav/tap/tavily
```

The tap auto-installs on first use; subsequent `brew upgrade` picks up new releases.

**One-line install (Linux / macOS)** — no Go toolchain required:

```sh
curl -sSL https://raw.githubusercontent.com/sapihav/tavily-cli/main/install.sh | bash
```

Downloads the latest release for your OS/arch, verifies SHA-256, installs `tavily` to `/usr/local/bin`. Override with `INSTALL_DIR=$HOME/.local/bin`. Requires `curl` + `jq`.

**From source** (requires Go 1.25+):

```sh
go install github.com/sapihav/tavily-cli@latest
```

The command produces a `tavily` binary in `$GOBIN` (or `$GOPATH/bin`).

## Auth

Set `TAVILY_API_KEY` in your environment. Get a key at
[app.tavily.com](https://app.tavily.com).

```sh
export TAVILY_API_KEY=tvly-...
```

If the variable is missing, `tavily` exits with code `2` and a pointer to the
provider's key page. There is no file fallback by design.

## Usage

```sh
tavily search "golang 1.25 release notes" --max-results 3 --pretty
```

### Example output

Every successful invocation emits a single JSON envelope on stdout:

```json
{
  "schema_version": "1",
  "provider": "tavily",
  "command": "search",
  "elapsed_ms": 412,
  "result": {
    "query": "golang 1.25 release notes",
    "answer": "",
    "results": [
      {
        "title": "Go 1.25 Release Notes",
        "url": "https://go.dev/doc/go1.25",
        "content": "Go 1.25 includes ...",
        "score": 0.92,
        "published_date": "2025-08-12"
      }
    ],
    "response_time": 0.41
  }
}
```

On error, nothing is written to stdout; a single human-readable line is
printed to stderr and the process exits with a non-zero code (see below).

## Flags

### `tavily search`

| Flag | Default | Description |
|---|---|---|
| `--max-results N` | `5` | Maximum results returned |
| `--search-depth` | `basic` | `basic` or `advanced` |
| `--topic` | `general` | `general` or `news` |
| `--include-answer` | `false` | Ask Tavily to include a synthesized answer |

### Global

| Flag | Default | Description |
|---|---|---|
| `--pretty` | `false` | Indent JSON output |
| `--out <file>` | stdout | Write JSON to a file instead of stdout |
| `--verbose` | `false` | Log progress to stderr |
| `--quiet` | `false` | Suppress non-error stderr output |
| `--max-retries N` | `3` | Retries on `429` / `5xx` with exponential backoff |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | success |
| `1` | API error (HTTP `>= 400`) |
| `2` | user / config error (missing key, invalid flag) |
| `3` | network error |

## Scope

Milestone 1 ships only `tavily search`. `extract`, `crawl`, `map`, and deep
research are deliberately out of scope for this release.

## License

MIT.
