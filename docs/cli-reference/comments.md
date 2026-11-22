---
title: "bd comments"
description: "View or manage comments on an issue"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc comments`.

View or manage comments on an issue.

Examples:
  # List all comments on an issue (issue id is required — there is no "comments list")
  bd comments bd-123

  # List comments in JSON format
  bd comments bd-123 --json

  # Add a comment
  bd comments add bd-123 "This is a comment"

  # Add a comment from a file
  bd comments add bd-123 -f notes.txt

```
bd comments [issue-id] [flags]
bd comments [command]
```

**Flags:**

```
      --kind string   List only comments of this kind, as structured records
      --last          With --kind: return only the most recent one
      --local-time    Show timestamps in local time instead of UTC
```

## bd comments add

Add a comment to an issue.

Examples:
  # Add a comment
  bd comments add bd-123 "Working on this now"

  # Add a comment from a file
  bd comments add bd-123 -f notes.txt

```
bd comments add [issue-id] [text...] [flags]
```

**Flags:**

```
  -a, --author string      Add author to comment
  -f, --file string        Read comment text from file
      --json-file string   Read a JSON comment body from a file (- for stdin); requires --kind
      --kind string        Store a kinded comment (e.g. handoff); read back with bd comments <id> --kind <kind>
```

## bd comments list

Invalid — use bd comments &lt;issue-id&gt; to list comments

```
bd comments list [flags]
```
