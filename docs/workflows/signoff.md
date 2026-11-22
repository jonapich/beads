---
title: Sign-off
description: Have agents summarize the calls they made when closing a bead, and review, approve or dispute each one after the fact.
---

An agent closing a bead often takes small design calls on your behalf: how
two mechanics share a formula, what got renamed, what got dropped. You find
out later, if at all. **Sign-off** makes those calls reviewable: the closing
agent leaves a summary, the bead waits for your verdict, and a dispute turns
into new work without reopening anything.

<Note>
Sign-off is in this fork's default branch, not in an upstream release. See
[Fork Status](/reference/fork-status).
</Note>

## Close with a summary

```bash
cat > summary.json <<'EOF'
{
  "calls_made": ["stun reuses the stagger formula"],
  "what_changed": "added the stun mechanic",
  "removed_or_renamed": ["daze -> stun"],
  "where_to_see": "arena, any heavy attack"
}
EOF
bd close bd-a1b2 --signoff-file summary.json   # or --signoff-file - for stdin
```

That adds the summary as a `bd-kind: signoff` comment, closes the bead, and
marks it pending. Each key holds a string or a list of strings; `commits` and
`captures` are optional extras. If the summary is already a comment, close
with `--signoff-needed` instead.

To require summaries, name the issue types or labels whose close needs one.
`bd close` then refuses those beads before closing anything, unless the agent
passes a reason to waive it:

```bash
bd config set signoff.types "feature,task,bug"
bd close bd-e5f6 --no-signoff "typo fix"      # closes, records the waiver
```

## Review

```bash
bd list --signoff pending        # closed beads waiting for you
bd signoff bd-a1b2               # state plus the summary, calls first
bd signoff bd-a1b2 --approve
bd signoff bd-a1b2 --dispute "stun and stagger must stay separate"
```

A dispute opens a follow-up bead titled `Dispute: <title>` with the same
priority, linked `discovered-from` the closed one, and comments on the closed
bead. The closed bead stays closed. `--request` marks an already-closed bead
pending.

`bd show` prints the summary above the description of a closed bead,
`bd show --context` includes it, and `bd prime` tells the next session how
many beads wait for sign-off.

## Where the state lives

| Metadata key | Value |
|---|---|
| `signoff` | `pending`, `approved`, `disputed` or `waived` |
| `signoff_at`, `signoff_by` | when and by whom the state was last set |
| `signoff_note` | the dispute text or the waiver reason |
| `signoff_followup` | the dispute's follow-up bead |

The `needs-signoff` label is added while a bead is pending and removed when
it is decided, so label-based views see the same set.
