---
title: M5 — `crawl` subcommand
type: task
priority: P2
status: todo
created: 2026-04-18
---

# M5 — `crawl` subcommand

## Problem Statement

MCP `tavily-crawl` wraps `POST /crawl` — BFS crawl from a root URL that returns extracted content for each page (superset of `map` which only returns URLs).

## Acceptance Criteria

- `tavily crawl <url>` — POST `/crawl`.
  - All M4 (`map`) flags apply: `--max-depth`, `--max-breadth`, `--limit`, `--instructions`, path/domain filters, `--allow-external`.
  - Plus extraction flags (from M3): `--extract-depth basic|advanced`, `--format markdown|text`, `--include-images`, `--include-favicon`, `--timeout SEC`, `--include-usage`.
- Envelope: `result.{base_url, results[].{url, raw_content, images[]?, favicon?}, response_time_s, usage?}`.
- Reuses flag-parsing helpers from M4.

## Context / Notes

- Endpoint: https://docs.tavily.com/documentation/api-reference/endpoint/crawl
- Depends on M3 + M4.
- Budget: ~300 LoC (most structs/helpers already exist).
- Network cost risk: a crawl can generate many page fetches. Consider a `--budget-pages N` client-side cap that aborts polling/streaming; add only if API exposes iterative results. Otherwise default `--limit` conservatively.
