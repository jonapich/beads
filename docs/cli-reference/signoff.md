---
title: "bd signoff"
description: "Review, approve or dispute a closed issue's sign-off summary"
---

{/* AUTO-GENERATED: do not edit manually */}

Generated from `bd help --doc signoff`.

Review, approve or dispute a closed issue's sign-off summary.

An agent that closes an issue may have made calls on the user's behalf. It
leaves a summary (bd close --signoff-file, or a 'bd-kind: signoff' comment) and
the issue awaits the user's sign-off. With no flag, signoff prints the state
and the latest summary.

  --approve           sign it off
  --dispute "text"    open a follow-up issue (discovered-from this one, same
                      priority) carrying the text; this issue stays closed
  --request           mark an already-closed issue as awaiting sign-off

State lives in metadata (signoff, signoff_at, signoff_by, signoff_note,
signoff_followup); the needs-signoff label marks the pending ones.

Examples:
  bd list --signoff pending
  bd signoff bd-123
  bd signoff bd-123 --approve
  bd signoff bd-123 --dispute "stun and stagger must share one formula"

```
bd signoff <id> [--approve | --dispute <text> | --request] [flags]
```

**Flags:**

```
      --approve          Sign off the closed issue
      --dispute string   Dispute the sign-off: opens a follow-up issue carrying this text
      --note string      With --approve: a note recorded as signoff_note
      --request          Mark a closed issue as awaiting sign-off
```
