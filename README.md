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

Covers the full [Tavily `/search` API surface](https://docs.tavily.com/documentation/api-reference/endpoint/search).

```sh
tavily search "golang 1.25" --search-depth advanced --topic news --time-range w --pretty
tavily search "acme earnings" --topic finance --include-answer advanced
tavily search "llm ops" --include-domain arxiv.org --include-domain openreview.net
tavily search "berlin" --include-images --include-image-descriptions
tavily search "climate" --include-raw-content --raw-content-format markdown
```

**Core**

| Flag | Default | Description |
|---|---|---|
| `--max-results N` | `5` | Maximum results returned |
| `--search-depth basic\|advanced\|fast\|ultra-fast` | `basic` | Depth tier |
| `--topic general\|news\|finance` | `general` | Search topic |
| `--auto-parameters` | `false` | Let Tavily auto-tune parameters |
| `--exact-match` | `false` | Require exact-match results |
| `--safe-search` | `false` | Filter NSFW |
| `--chunks-per-source N` | `0` | Number of chunks returned per source |

**Filters**

| Flag | Default | Description |
|---|---|---|
| `--time-range d\|w\|m\|y` | unset | Relative time window |
| `--start-date YYYY-MM-DD` | unset | Absolute start date |
| `--end-date YYYY-MM-DD` | unset | Absolute end date |
| `--include-domain DOMAIN` | — | Repeatable (≤300 entries) |
| `--exclude-domain DOMAIN` | — | Repeatable (≤150 entries) |
| `--country NAME` | unset | Boost results from country (`topic=general` only) |

**Enrichment**

| Flag | Default | Description |
|---|---|---|
| `--include-answer basic\|advanced` | unset | Ask for a synthesized answer. Bare flag = `basic`; legacy `--include-answer=true` also maps to `basic` |
| `--include-images` | `false` | Return image URLs |
| `--include-image-descriptions` | `false` | Require `--include-images` |
| `--include-raw-content` | `false` | Return full raw page content |
| `--raw-content-format markdown\|text` | `markdown` | Raw content format |
| `--include-favicon` | `false` | Include each result's favicon URL |

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

Milestone 2 shipped: `tavily search` covers the full `/search` API (topic,
depth, time/date ranges, domain filters, country, images, raw content,
chunks, favicon). On the backlog: `extract`, `map`, `crawl`, research.

## License

MIT.
