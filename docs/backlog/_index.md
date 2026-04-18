# tavily-cli Backlog

## Active

_(work currently in flight)_

## Up Next

1. [M2 — `search` flag parity with Tavily API](tasks/upgrade-search-flags.md) — P1
2. [M3 — `extract` subcommand](tasks/add-extract-subcommand.md) — P1 (MCP `tavily-extract`)
3. [M4 — `map` subcommand](tasks/add-map-subcommand.md) — P2 (MCP `tavily-map`)
4. [M5 — `crawl` subcommand](tasks/add-crawl-subcommand.md) — P2 (MCP `tavily-crawl`, depends on M3+M4)
5. [M6 — contract hardening](tasks/add-contract-hardening.md) — P2 (workspace standard)

## Backlog

_(known work, not yet prioritized)_

## Ideas

- `tavily research` — wraps `/research` endpoint (not in Tavily MCP today); `--model mini|pro|auto`, `--citation-format`, `--output-schema @file`, `--stream` (SSE → line-delimited JSON).

<!-- Done items are not tracked here. Completed items have status: done in their frontmatter. Files remain in tasks/bugs/ for historical reference. -->
