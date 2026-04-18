---
title: M3 — `extract` subcommand
type: task
priority: P1
status: todo
created: 2026-04-18
---

# M3 — `extract` subcommand

## Problem Statement

MCP `tavily-extract` wraps `POST /extract` to pull clean content from one or more URLs. The CLI has no extract command — agents cannot get URL contents without shelling to a different tool.

## Acceptance Criteria

- `tavily extract <url> [<url>...]` — POST `/extract`.
  - Accepts URLs as args, newline-separated on stdin (`-`), or comma-separated via `--urls`.
  - Flags: `--query` (focus extraction on this query), `--extract-depth basic|advanced`, `--format markdown|text`, `--chunks-per-source 1..5`, `--include-images`, `--include-favicon`, `--timeout SEC`, `--include-usage`.
- Envelope: `result.results[].{url, raw_content, images[]?, favicon?}` + `result.failed_results[]` + optional `result.usage`.
- Standard flags (`--dry-run`, etc.) supported.

## Context / Notes

- Endpoint: https://docs.tavily.com/documentation/api-reference/endpoint/extract
- Budget: ~250 LoC.
- Batch-capable: send all URLs in one request.
