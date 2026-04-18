---
title: M6 — contract hardening (`--dry-run`, `--json-errors`, `schema`)
type: task
priority: P2
status: todo
created: 2026-04-18
---

# M6 — contract hardening

## Problem Statement

Workspace contract mandates `--dry-run`, `--json-errors`, `--rate-limit`, `--timeout`, `--user-agent`, stdin `-`, and a `schema` subcommand on every CLI. `tavily-cli` currently lacks all of these. They must be added before the CLI is publishable.

## Acceptance Criteria

- `tavily schema` — emits full command tree as JSON.
- `--dry-run` on every subcommand: prints method + URL + headers (key redacted) + body, exits 0.
- `--json-errors` on every subcommand: stderr becomes `{error:{message,code,hint?,docs_url?}}`.
- `--rate-limit N/s` (default 2/s per ROADMAP §5.4).
- `--timeout SEC` (default 30s for search, 60s for extract/map, 300s for crawl).
- `--user-agent` flag + `TAVILY_USER_AGENT` env override.
- stdin `-` for query (search) and URLs (extract/map/crawl).

## Context / Notes

- Runs in parallel with M2–M5, but must land before `v0.2.0`.
- Budget: ~300 LoC.
- Add golden test asserting the bearer token never appears in `--verbose` or `--dry-run` output on any command.
