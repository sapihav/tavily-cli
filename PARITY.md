# tavily-cli Parity Matrix

Capability mapping across the Tavily HTTP API, the official Tavily MCP server, and the `tavily` CLI.

Last audited: 2026-04-25
Sources: [docs.tavily.com/documentation/api-reference](https://docs.tavily.com/documentation/api-reference) (+ [llms.txt](https://docs.tavily.com/llms.txt)), [github.com/tavily-ai/tavily-mcp](https://github.com/tavily-ai/tavily-mcp), local `tavily --help` (no `schema` cmd yet — see M6).

## Matrix

| API endpoint | MCP tool | CLI command | Status | Notes |
|---|---|---|---|---|
| `POST /search` | `tavily-search` | `tavily search` | shipped (M1+M2) | Full Tavily `/search` parity: topic, search-depth (basic/advanced/fast/ultra-fast), include-answer, include-images/-image-descriptions, include-raw-content, raw-content-format, include-favicon, include/exclude-domain, country, time-range, start/end-date, max-results, chunks-per-source, exact-match, auto-parameters, safe-search. |
| `POST /extract` | `tavily-extract` | `tavily extract` | shipped (M3) | Batched multi-URL; flags: extract-depth, format, include-images, include-favicon, include-usage, query (focus), chunks-per-source, timeout. URLs via args, `--urls`, or stdin `-`. |
| `POST /map` | `tavily-map` | `tavily map` | shipped (M4) | Flags: max-depth, max-breadth, limit, instructions, select/exclude-path (repeatable regex), select/exclude-domain (repeatable regex), allow-external, dry-run, timeout. URL via positional arg or stdin `-`. |
| `POST /crawl` | `tavily-crawl` | `tavily crawl` | planned (M5) | Backlog: `add-crawl-subcommand.md`, P2, depends on M4. Inherits all map flags + extract flags. |
| `POST /research` | n/a (not in MCP) | `tavily research` | planned (Ideas) | Backlog `_index.md` Ideas section. Flags sketched: `--model`, `--citation-format`, `--output-schema @file`, `--stream`. Not prioritized. |
| `GET /research/{request_id}` | n/a | — | planned (Ideas)? | Implied companion to `/research` for async polling. Not explicitly on backlog. |
| `POST /logs` | n/a | — | skipped (no demand) | Internal usage-tracking endpoint. Not on backlog. |
| `GET /usage` | n/a | — | skipped (no demand) | Account usage details. Not on backlog. |
| `POST /generate-keys` (enterprise) | n/a | — | n/a | Enterprise key mgmt; out of scope for an agent-facing CLI. |
| `GET /key-info` (enterprise) | n/a | — | n/a | Enterprise key mgmt; out of scope. |
| `POST /deactivate-keys` (enterprise) | n/a | — | n/a | Enterprise key mgmt; out of scope. |
| — | — | `tavily schema` | planned (M6) | Workspace-standard self-describing JSON command tree. Backlog: `add-contract-hardening.md`. |
| — | — | `tavily version` | shipped | Prints CLI version as JSON. |
| — | — | `tavily completion` | shipped | Cobra-generated shell completion. |

## Gaps

Items present in the upstream API or MCP that have no CLI counterpart and are not yet on the backlog:

- `POST /logs` — no demand identified; agents typically don't need to read account logs from a search CLI.
- `GET /usage` — would be a small win for `tavily usage` (account quota visibility); not on backlog. Worth a P3 idea.
- `GET /research/{request_id}` — `/research` is on the Ideas list but the polling companion isn't called out explicitly; if `tavily research` ships sync-only it's `n/a`, otherwise it should be tracked alongside.
- Contract flags (`--dry-run`, `--json-errors`, `--rate-limit`, persistent `--timeout`, `--user-agent`, stdin `-` on all commands) — tracked under M6 but not yet shipped; CLI is currently below the workspace-wide contract bar.

All four MCP tools (`tavily-search`, `tavily-extract`, `tavily-map`, `tavily-crawl`) are either shipped or planned — no MCP-tool gap.
