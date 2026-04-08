# Dry-Run Design

**Date:** 2026-04-08  
**Status:** Approved

## Overview

Add `--dry-run` and `--verbose` flags to `dnstonetbox`. In dry-run mode the tool fetches from NetBox, renders all output, and compares against current on-disk files — but writes nothing and runs no reload commands. With `--verbose`, a unified diff is printed for each file that would change.

## Flags

Two new flags added to `main.go`:

- `--dry-run` — skip all writes and reload commands; exit after one pass (implies `--once` behavior)
- `--verbose` — when combined with `--dry-run`, print a unified diff for each file that would change or be created; without `--dry-run`, log extra detail during normal runs (e.g. "zone X unchanged, skipping")

## Generator Signature Change

All three `Sync` functions gain two trailing booleans:

```go
func Sync(cfg Config, hosts []model.Host, dryRun, verbose bool) error
```

Affected packages: `nsd`, `unbound`, `dhcpd`.

## Behavior Table

| Situation | Normal | `--dry-run` | `--dry-run --verbose` |
|---|---|---|---|
| File unchanged | silent | `[dry-run] <svc>: <target> unchanged` | same, no diff |
| File would change | write + reload | `[dry-run] <svc>: <target> would be updated` | same + unified diff |
| File does not exist | write + reload | `[dry-run] <svc>: <target> would be created` | same + full content as diff |

For NSD, `<target>` is the zone name. For unbound and dhcpd, it is the config file path.

## Diff Implementation

No new dependencies. A `unifiedDiff(label, old, new []byte) string` helper is added to each generator package (or a shared internal helper — same logic appears in all three). It:

1. Splits old and new content into lines
2. Produces standard `diff -u` format: `--- old`, `+++ new`, `@@ -L,N +L,N @@` hunks with 3 lines of context

The diff is written to stdout via `log.Printf` or `fmt.Print` alongside the "would be updated" log line.

## File Layout

No new files required. Changes are confined to:

- `main.go` — add flags, pass `dryRun`/`verbose` to each `Sync` call
- `nsd/generator.go` — update `Sync` signature, add dry-run branch + diff helper
- `unbound/generator.go` — same
- `dhcpd/generator.go` — same

## Out of Scope

- `--verbose` without `--dry-run` producing diffs (it only adds "unchanged" log lines in normal mode)
- Colorized diff output
- Machine-readable (JSON) output
