---
title: M2 — `search` flag parity with Tavily API
type: task
priority: P1
status: done
created: 2026-04-18
---

# M2 — `search` flag parity with Tavily API

## Problem Statement

Tavily's `/search` endpoint supports many filters (topic, depth, time range, domains, country, images, raw content, chunks) that the CLI does not expose. Agents cannot replicate the MCP `tavily-search` tool today because only four flags are wired up.

## Acceptance Criteria

Extend `tavily search` with:

- `--topic general|news|finance` (currently limited to general/news).
- `--search-depth basic|advanced|fast|ultra-fast` (currently basic/advanced only).
- `--time-range d|w|m|y`.
- `--start-date YYYY-MM-DD`, `--end-date YYYY-MM-DD`.
- `--include-domain DOMAIN` (repeatable, ≤300), `--exclude-domain DOMAIN` (repeatable, ≤150).
- `--country ISO`.
- `--include-images`, `--include-image-descriptions`.
- `--include-raw-content` / `--raw-content-format markdown|text`.
- `--include-answer basic|advanced` (currently boolean only).
- `--chunks-per-source N`, `--auto-parameters`, `--exact-match`, `--safe-search`, `--include-favicon`.

Output envelope adds `result.images[]?`, `result.raw_content?` fields when requested.

## Context / Notes

- Endpoint: https://docs.tavily.com/documentation/api-reference/endpoint/search
- Budget: ~350 LoC (mostly flag wiring + struct expansion + test updates).
- Pure flag expansion on existing command — no new subcommand.
