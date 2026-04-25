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

### `tavily extract`

Wraps the [Tavily `/extract` API](https://docs.tavily.com/documentation/api-reference/endpoint/extract).
URLs can be passed as positional args, comma-separated via `--urls`, or
newline-separated on stdin (`-`). Duplicates and blanks are dropped; all URLs
go out in a single batched request.

```sh
tavily extract https://example.com --pretty
tavily extract https://a.example https://b.example --extract-depth advanced --format text
printf "https://x\nhttps://y\n" | tavily extract - --include-images --include-favicon
tavily extract --urls https://a.example,https://b.example --query "pricing" --include-usage
```

| Flag | Default | Description |
|---|---|---|
| `--urls a,b,c` | unset | Comma-separated list of URLs (in addition to positional args) |
| `--query STRING` | unset | Focus extraction on this query |
| `--extract-depth basic\|advanced` | `basic` | Extraction depth tier |
| `--format markdown\|text` | `markdown` | Output format for `raw_content` |
| `--chunks-per-source N` | `0` | Max chunks per source, 1-5 (0 = server default) |
| `--include-images` | `false` | Return image URLs pulled from each page |
| `--include-favicon` | `false` | Return each source's favicon URL |
| `--include-usage` | `false` | Include per-request usage accounting |
| `--timeout SEC` | `60` | Per-request timeout in seconds |

### `tavily map`

Wraps the [Tavily `/map` API](https://docs.tavily.com/documentation/api-reference/endpoint/map).
Given a root URL, returns a flat list of discovered URLs. Pass `-` to read the
URL from stdin. Path/domain filters take regex patterns and are repeatable.

```sh
tavily map https://docs.example.com --max-depth 3 --pretty
tavily map https://x.example --select-path '^/docs/.*' --exclude-path '^/admin/.*'
echo https://x.example | tavily map - --instructions "focus on the API reference" --dry-run
```

| Flag | Default | Description |
|---|---|---|
| `--max-depth N` | server | Crawl depth from the root, 1-5 |
| `--max-breadth N` | server | Links followed per page, 1-500 |
| `--limit N` | server | Total link cap before stopping |
| `--instructions STRING` | unset | Natural-language guidance for the crawler |
| `--select-path REGEX` | — | Repeatable; include only matching paths |
| `--exclude-path REGEX` | — | Repeatable; drop matching paths |
| `--select-domain REGEX` | — | Repeatable; include only matching domains |
| `--exclude-domain REGEX` | — | Repeatable; drop matching domains |
| `--allow-external` | `true` | Follow links to external domains (server default) |
| `--dry-run` | `false` | Print the planned request body and exit; no network call |
| `--timeout SEC` | `60` | Per-request timeout in seconds |

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

Milestones 1–4 shipped: `tavily search` covers the full `/search` API,
`tavily extract` wraps `/extract` (batched multi-URL, focus query, images,
favicon, usage), and `tavily map` wraps `/map` (URL graph mapping with
path/domain regex filters). On the backlog: `crawl`, research.

## License

MIT.
