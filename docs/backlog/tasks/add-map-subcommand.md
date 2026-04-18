---
title: M4 — `map` subcommand
type: task
priority: P2
status: todo
created: 2026-04-18
---

# M4 — `map` subcommand

## Problem Statement

MCP `tavily-map` wraps `POST /map` — given a root URL, returns a structured sitemap-like graph of URLs. Useful for agents that need to enumerate a site before deciding what to extract.

## Acceptance Criteria

- `tavily map <url>` — POST `/map`.
  - Flags: `--max-depth 1..5` (default 2), `--max-breadth 1..500`, `--limit N`, `--instructions "..."` (natural-language guidance for crawler).
  - Path filters: `--select-path REGEX` (repeatable), `--exclude-path REGEX` (repeatable).
  - Domain filters: `--select-domain DOMAIN` (repeatable), `--exclude-domain DOMAIN` (repeatable).
  - `--allow-external` (bool) — follow links off the root domain.
- Envelope: `result.{base_url, urls[], response_time_s}`.

## Context / Notes

- Endpoint: https://docs.tavily.com/documentation/api-reference/endpoint/map
- Budget: ~250 LoC.
- Extract shared regex/CSV flag parsing into `internal/flagutil` — will be reused by M5.
