---
title: "bd comment"
description: "Add a comment to an issue"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc comment`.

Add a comment to an issue.

Shorthand for 'bd comments add &lt;id&gt; "text"'.

The comment is written as the actor unless you name an author, exactly as with
'bd comments add': --author wins, then BEADS_COMMENT_AUTHOR, then the actor.
--kind stores a kinded comment (see 'bd comments add --help').

Examples:
  bd comment bd-123 "Working on this now"
  bd comment bd-123 Working on this now
  echo "comment from pipe" | bd comment bd-123 --stdin
  bd comment bd-123 --file notes.txt
  bd comment bd-123 "Tests are green" --author 'coder · Sonnet 5.5 · high'

Note: "comment" (singular) only adds a comment — it has no "list" subcommand.
To list comments on an issue, use the plural form: bd comments &lt;id&gt;

```
bd comment <id> [text...] [flags]
```

**Flags:**

```
  -a, --author string      Author to record on the comment (default: $BEADS_COMMENT_AUTHOR, else the actor)
      --file string        Read comment text from file
      --json-file string   Read a JSON comment body from a file (- for stdin); requires --kind
      --kind string        Store a kinded comment (e.g. handoff); read back with bd comments <id> --kind <kind>
      --stdin              Read comment text from stdin
```
