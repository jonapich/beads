---
title: Fork Status
description: What this custom build's default branch adds over the last upstream beads release, and how the docs track it.
---

This site documents a custom build of beads: the default branch of
[jonapich/beads](https://github.com/jonapich/beads), a fork kept separate to
protect against supply chain attacks. The docs and the
[CLI Reference](/cli-reference/index) describe that branch as it stands, not
an upstream release. `bd version` names the commit you run.

## Base

The fork tracks upstream beads through reviewed merges. The default branch
contains upstream's main as of 2026-11-08, after the v1.3.4 release, plus the
changes below.

## Added in this fork

| Change | Where it is documented |
|---|---|
| Builds and tests run through `docker buildx bake`; nothing reaches upstream infrastructure | [Installation](/getting-started/installation) |
| Embedded Dolt collects garbage automatically, and `bd dolt push` skips a push with nothing to send | [Configuration](/reference/configuration#automatic-garbage-collection) |
| `bd prime`'s default briefing stays under the hook output limit | [IDE setup](/getting-started/ide-setup) |
| `bd show --field` / `--section` / `--context`, stages, typed links, kinded comments, the awaiting-user inbox, and stale-claim listing | [Agent Working Memory](/multi-agent/agent-memory) |
| Post-close sign-off: `bd close --signoff-file`, `bd signoff`, `bd list --signoff` | [Sign-off](/workflows/signoff) |

## How these docs stay current

Every change to the default branch updates these pages in the same commit,
and regenerates the CLI Reference from the branch's own `bd`
(`docs/cli-docs.pin` is set to `HEAD`).
