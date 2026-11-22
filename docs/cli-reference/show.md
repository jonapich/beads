---
title: "bd show"
description: "Show issue details"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc show`.

Show issue details

```
bd show [id...] [--id=<id>...] [--current] [flags]
```

**Aliases:** view

**Flags:**

```
      --as-of string           Show issue as it existed at a specific commit hash or branch (requires Dolt; commit hashes must be the full 32-character hash from 'bd history', not a prefix)
      --brief-deps             Reduce each dependency to its identity fields in JSON output (--json only; drops description, design, notes and acceptance criteria)
      --children               Show only the children of this issue
      --comments-tail int      Render only the last N comments in text output, preceded by one elision line naming how many older ones were hidden (text output only; 0, the default, renders every comment unchanged; N must be >= 0)
      --context                Print a compact, size-bounded briefing for an agent starting cold: the issue's text, its parent's decisions, deps, children, links, metadata and recent comments
      --context-comments int   With --context: number of most recent comments to include (default 3) (default 3)
      --current                Show the currently active issue (in-progress, hooked, or last touched)
      --field string           Print one field raw: description, design, notes, acceptance, title, status, type, priority, assignee, stage, labels, metadata, or metadata.KEY
      --id stringArray         Issue ID (use for IDs that look like flags, e.g., --id=gt--xyz)
      --include-comments       Stream full comment bodies in JSON output (--json only; may be slow on issues with many comments). Without it the comments field is absent entirely, not truncated: an issue with comments carries comment_count and comments_omitted=true, so check those before reading an absent comments field as none.
      --include-dependents     Stream full dependent issues in JSON output (--json only; may be slow on hub beads)
      --local-time             Show timestamps in local time instead of UTC (date-only fields are always local)
      --long                   Show all available fields (extended metadata, agent identity, gate fields, etc.)
      --max-chars int          Character budget for --context (truncated parts name the command that fetches them in full) (default 8000)
      --refs                   Show issues that reference this issue (reverse lookup)
      --section string         Print one markdown section (heading through the next heading of the same or higher level). Searches --field, or description, design, notes and acceptance in that order
      --short                  Show compact one-line output per issue
      --thread                 Show full conversation thread (for messages)
  -w, --watch                  Watch for changes and auto-refresh display
```
