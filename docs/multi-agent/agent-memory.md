---
title: Agent Working Memory
description: Let agents that start cold read exactly the part of a bead they need, and leave stages, links, handoffs and claims on it for the next agent.
---

When a dispatcher hands a bead to a fresh agent, the bead is the agent's
whole memory: the plan, the decisions already taken, the mocks to match, and
what the last agent got through. Pasting a 50 KB epic into every prompt costs
tokens and loses detail. These commands keep the bead itself as the shared
memory between agents: reads that fetch one piece at a time, and structured
writes the next agent can read back. Everything accepts `--json`.

<Note>
These commands are in this fork's default branch, not in an upstream release.
See [Fork Status](/reference/fork-status).
</Note>

## Read one piece of a bead

`--field` prints one field raw, with nothing around it. `--section` prints
one markdown section: the heading line through the line before the next
heading of the same or a higher level.

```bash
bd show bd-a1b2 --field acceptance        # also: description, design, notes, title,
                                          # status, stage, labels, metadata, metadata.KEY
bd show bd-a1b2 --section "Step 2"        # searches description, design, notes, acceptance
bd show bd-a1b2 --field design --section "Risks"
bd show bd-a1b2 --field title --json      # {"id":"bd-a1b2","field":"title","value":"..."}
```

Section matching ignores case, prefers an exact heading over a prefix match,
and skips headings inside code fences. A missing section exits 1 and lists
the headings that do exist.

## Brief an agent that starts cold

`bd show --context` prints one compact bundle sized for a prompt:

```bash
bd show bd-a1b2 --context --max-chars 6000
```

```text
# bd-a1b2 [task P1 open] Add the stun mechanic
stage: code (since 2026-11-22T14:28:46Z) | labels: combat
parent: bd-9f00 [open] Combat epic

## description
...
## design
...
[truncated: bd show bd-a1b2 --field design]

## acceptance
...
## parent decisions (bd-9f00)
## Decisions
- stun and stagger share one formula
## deps
- bd-77c1 [closed] Hit detection
## children
- bd-a1b2.1 [open] Stun VFX
## links
- mock https://example.com/stun.png — v2
## comments (last 3 of 9)
- 2026-11-22 14:30 planner [handoff]: {"summary":"..."}
```

The bundle holds the bead's own text, its parent's decisions (a
`Decisions` section of the parent's notes, design or description, else the
parent's notes), dependencies and children by id, status and title, typed
links, metadata, and the last few comments (`--context-comments N`, default
3). When it would exceed `--max-chars` (default 8000), the longest parts are
cut first, so a long design never pushes out the acceptance criteria. Every
cut part ends with the command that prints it in full, and `--json` lists the
cuts under `truncated`.

## Stages

A stage records where a bead is in your pipeline, so an agent that replaces
a crashed one knows where to resume.

```bash
bd config set stage.values "scout,plan,code,test,review,verify,done"
bd update bd-a1b2 --stage code
bd list --stage review
```

A stage change writes three metadata keys: `stage`, `stage_changed_at`, and
`stage_at.<stage>` (the last time the bead entered that stage). The update
history records every change. With `stage.values` unset, any
`[a-z][a-z0-9_]*` name is accepted.

## Typed links

Mocks, artifacts, captures, commits and docs get their own links instead of
being buried in comments.

```bash
bd link add bd-a1b2 --kind mock --url https://example.com/stun.png --note "v2"
bd link list bd-a1b2 --json
bd link rm bd-a1b2 --url https://example.com/stun.png
bd list --has-link mock
```

Links are stored as metadata arrays named `link.<kind>`, each entry
`{url, note, at, by}`. Adding a URL that is already linked under that kind
updates its note. Allowed kinds come from `link.kinds`. `bd link <id1> <id2>`
still adds a dependency.

## Handoff comments

A kinded comment carries structured output from one agent to the next: a
plan, review findings, a test report.

```bash
bd comments add bd-a1b2 --kind handoff --author planner --json-file plan.json
echo '{"findings":[]}' | bd comments add bd-a1b2 --kind review --author reviewer --json-file -
bd comments bd-a1b2 --kind handoff --json          # every handoff, data parsed
bd comments bd-a1b2 --kind handoff --last --json   # the latest one only
```

The kind is the first line of the stored comment (`bd-kind: handoff`), so
kinded comments export, import and sync like any comment.

## Waiting on the user

Beads that need a human decision should stay out of agents' queues and show
up together for the human.

```bash
bd label add bd-c3d4 needs-decision
bd list --awaiting-user                                   # mock/artifact-linked ones first
bd config set ready.exclude_labels "needs-decision,needs-design"
bd ready                                                  # never shows them now
```

`awaiting_user.labels` sets which labels count. Beads with a mock or artifact
link sort first, because they are ready to judge.

## Spotting an abandoned claim

`bd update --claim` takes a lease that `bd heartbeat` keeps alive. When a
session is known (`--session` or `CLAUDE_SESSION_ID`), the claim also records
`claim.session` and `claim.at` in metadata.

```bash
bd list --stale-claims 30m     # lease expired over 30m ago, or no lease and quiet for 30m
bd reclaim --older-than 30m    # hand those back to the ready queue
```
