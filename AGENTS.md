# Beads — Contributor Agent Instructions

This file is for agents and humans **developing beads** (`bd`). If you are
using `bd` to track work in your own project, read the user docs instead:
[docs site](https://github.com/jonapich/beads/tree/main/docs) and
[IDE and agent setup](docs/getting-started/ide-setup.md).

<!-- Contributor-only file. Do not run `bd setup` or `bd init` against this
checkout: they would inject the end-user beads integration block here. -->

## This is a fork

This checkout is jonapich/beads, a personal fork of gastownhall/beads kept
separate to protect against supply chain attacks. All work stays in the fork.

- Never write to the original project: no issues, PRs, comments, or pushes.
- Changes from the original arrive only through `scripts/fork/sync-upstream.ps1`,
  which leaves the merge uncommitted for review.
- Nothing may fetch from, or send data to, the original project's
  infrastructure. `docker buildx bake guard` enforces this. Paths the fork does
  not carry are listed in `scripts/fork/removed-paths.txt`.
- `engdocs/` still describes the original project's Bazel and CI setup, and the
  contributor and maintainer policy below was written for the original project.
  Treat both as background.
Nested `AGENTS.md` files hold area-specific rules; the routing table at the
end lists them. `CLAUDE.md` files are symlinks to their sibling `AGENTS.md`.

## How work flows here

GitHub Issues is the public tracker. Use an issue when it adds context
reviewers need: a user-visible bug, a behavior or design change worth
discussing, or work that spans several PRs. The issue carries the
reproduction, impact, and evidence for a bug, or the motivation, impact, risk,
and verification plan for a change. Small, self-explanatory changes (typos,
flaky tests, refactors, CI or docs tweaks) can go straight to a PR whose body
explains the why.

1. If the change warrants an issue, find or file one with the bug or feature
   form. It does not need maintainer approval first; file it before or
   alongside the PR.
2. Work on a branch, open a PR against `main` (its body says
   `Closes #<issue>` when there is one), and let CI and review run.
3. Maintainers triage issues with `status/needs-triage`, `status/needs-info`,
   `status/needs-repro`, `status/needs-design`, and `status/accepted`
   (confirmed); see [CONTRIBUTING.md](CONTRIBUTING.md).

Maintainers also keep an internal bd ledger. It is optional for contributors
and not a substitute for the GitHub issue.

## Agent contribution policy

- File an issue when the change warrants one (see above). When you do, use
  the bug or feature form fields, fill each from evidence, and answer
  `NOT_ENOUGH_INFO` where the evidence runs out.
- A human reviews and stands behind every issue and PR an agent drafts.
  Evidence that the change works end-to-end is required; "unit tests pass"
  alone is not evidence.
- Before implementing work, opening a PR, or merging/closing a PR, run the PR
  preflight:
  ```bash
  scripts/pr-preflight.sh --search "<topic keywords>" --repo jonapich/beads
  scripts/pr-preflight.sh <pr-number> --repo jonapich/beads
  ```
  The preflight is the agent gate for PR handling; do not rely on
  auto-discovery of CONTRIBUTING.md.
- Use `gh` for GitHub issues and PRs, not browser tools. Write PR, issue,
  comment, and review bodies to a file, run `scripts/gh-body-lint <body-file>`,
  and pass the file with `gh ... --body-file`. Sign agent-written GitHub
  comments and reviews per [engdocs/AGENT_SIGNING.md](engdocs/AGENT_SIGNING.md).

## Contributor protection

External contributor PRs have priority. Read [CONTRIBUTING.md](CONTRIBUTING.md)
— it contains promises we've made to contributors — and, before triaging,
reviewing, landing, closing, or otherwise maintaining PRs,
[PR_MAINTAINER_GUIDELINES.md](PR_MAINTAINER_GUIDELINES.md). The maintainer
policy is to maximize community throughput: find useful contributor value,
absorb or transform it locally when practical, preserve attribution, and use
request-changes only as a last resort.

Before implementing any feature or fix, check for existing open PRs on the
same topic. If one exists:

1. **Review it first** — read the diff, understand the approach.
2. **Build on their work, don't rewrite it** — check out their branch,
   fix/adapt as needed.
3. **Preserve their tests** — keep them unless they're wrong.
4. **Attribute properly** — `Co-authored-by:` in commits, reference their PR.
5. **Never close or supersede their PR silently.** If a rewrite is
   unavoidable, explain why on the original PR and credit their design/tests.

## Scope: where a change belongs

Read [engdocs/PROJECT_CHARTER.md](engdocs/PROJECT_CHARTER.md) before adding
feature surface area. Beads owns issue tracking primitives and should not
encode orchestration-layer policy, become a storage engine, or casually expand
the database schema when metadata would work. Prefer, in order: issue
metadata over new fields, a flag on an existing command over a new command,
an integration or plugin over core, and leave orchestration policy to the
orchestration layer.

**Storage boundary.** Beads talks to storage through a driver interface
(`dolthub/driver` for Dolt). Do not add beads-side flocks, engine
introspection, storage-specific retry or crash-recovery logic, or public SDK
return types that leak driver internals. If the boundary is too narrow, widen
the interface or route the issue to the driver instead of patching around it
in beads. One maintainer-granted exception: `databaseLocked` in
`internal/doltserver` takes a shared, non-blocking lock on Dolt's own
`.dolt/noms/LOCK` and releases it at once (it never creates or writes the
file), only to decide whether the workspace's Dolt database directory (the
store's CLI directory: `.beads/dolt/<db>` by default, or `<db>` under a
custom `dolt_data_dir` or under the shared server's data directory) is served
by a live dolt process, so bd runs the dolt CLI there only for a served
directory. A busy lock (another process holds it, as every dolt process with
the database open does) counts as served. A probe
that cannot open the file, or whose lock call fails for any other reason,
counts as not served, which sends bd through the SQL server (the safe
direction). The exception ends once the Dolt driver can report whether a
directory is served, and covers no other lock (the test-only
`testutil.HoldDoltDatabaseLock` is outside it). The same proof also reads
Dolt's `.dolt/sql-server.info` for the port and PID it names, as the dolt CLI
does, the way bd already reads the remotes in Dolt's repo state file
(`PersistedRemoteInfos`). `bd doctor` support for
embedded mode is enabled one subcommand at a time, each human-vetted
(GH#3794): do not lift the embedded-mode gate in
`cmd/bd/doctor.go` wholesale, and keep database-layer checks and fixes
server-gated until the driver interface covers them.

**Layering.** Schema → `internal/storage/issueops` → `cmd/bd`. Add new
primitives at the lowest layer first, one layer per PR, stacked when a fix
spans layers. See
[CONTRIBUTING_PR_GUIDELINES.md](CONTRIBUTING_PR_GUIDELINES.md).

## Build, test, lint

Builds and tests run through `docker-bake.hcl`, so the host needs only Git and
Docker. Do not install Go, Bazel, make, or compilers on the host.

```powershell
docker buildx bake         # guard, then dist/windows-amd64/bd.exe
docker buildx bake test    # the Go test suite, in a container
docker buildx bake guard   # fails on references to the original project's infrastructure
$env:BD_TEST_ARGS = "./internal/config/..."; docker buildx bake test   # one package
```

- A passing run says nothing about a test that never ran, such as one skipped or
  matched by no filter. Count it only when the log shows `--- PASS: TestName`.
  [engdocs/TESTING.md](engdocs/TESTING.md) covers test design.
- **Never pollute a production database with test issues.** Use `t.TempDir()`
  in Go tests and a disposable working directory for manual `bd` experiments.
- All new features need tests.
- If you changed behavior, update the user docs (`docs/`, via the
  `beads-docs` skill) or README in the same PR.
## Commits and PRs

- Conventional Commits: `type(scope): summary` (`fix`, `feat`, `docs`,
  `test`, `refactor`, `chore`, `ci`, `perf`).
- For agent-prepared commits, include the `Agent-Signature:` trailer described
  in [engdocs/AGENT_SIGNING.md](engdocs/AGENT_SIGNING.md). Use
  `unknown-model` or `unknown-reasoning` when reliable runtime metadata is
  unavailable.
- Maintainers working from the internal ledger may also end the subject with
  the bead ID, e.g. `(bd-abc)`, so `bd doctor` can detect orphaned issues.
- Work on a feature branch and open a PR against `main`; direct pushes to
  `main` are reserved for releases and narrow operational fixes.

## Ending a session

Do not push to `main`. Before handing off:

1. Run the quality gates your change needs (`docker buildx bake test` and
   `docker buildx bake guard`). If gates are broken on `main`, report it as a
   P0 issue.
2. Record follow-up work as GitHub issues (or ledger beads, for maintainers).
3. Commit, push, or open a PR only when the person you are working for asked
   you to. Report changed files, validation run, and anything left open.

## CLI output

**NEVER use emoji-style icons** (🔴🟠🟡🔵⚪) in CLI output. **ALWAYS use small
Unicode symbols** with semantic colors: status `○ ◐ ● ✓ ❄`; priority as a
`P0`–`P4` label with color (no status glyph). The full visual design system
and CLI design principles are in
[engdocs/UI_PHILOSOPHY.md](engdocs/UI_PHILOSOPHY.md).

## Non-interactive shell commands

Agents cannot answer prompts. Use `cp -f`, `mv -f`, `rm -f`, `rm -rf`,
`cp -rf` (these may be aliased to `-i`); `scp`/`ssh -o BatchMode=yes`;
`apt-get -y`; `HOMEBREW_NO_AUTO_UPDATE=1 brew`. **DO NOT use `bd edit`** — it
opens `$EDITOR`; use `bd update <id> --title/--description/--design/--acceptance/--append-notes`
(pipe text with special characters via `--description=-`).

## Routing table

| When you are touching… | Read first |
|---|---|
| `cmd/bd/` (commands, output, proxied-server routes) | [cmd/bd/AGENTS.md](cmd/bd/AGENTS.md) |
| `engdocs/` (contributor and internals docs) | [engdocs/AGENTS.md](engdocs/AGENTS.md) |
| `docs/` (the published user docs) | `.claude/skills/beads-docs/SKILL.md` |
| Any Go test | [engdocs/TESTING.md](engdocs/TESTING.md) |
| A new storage operation or issueops role | [engdocs/ADDING_AN_ISSUEOPS_ROLE.md](engdocs/ADDING_AN_ISSUEOPS_ROLE.md) |
| Schema migrations | [internal/storage/schema/migrations/README.md](internal/storage/schema/migrations/README.md) |
| Dolt concurrency, server vs embedded mode | [engdocs/design/dolt-concurrency.md](engdocs/design/dolt-concurrency.md) |
| Errors returned to users | [engdocs/ERROR_HANDLING.md](engdocs/ERROR_HANDLING.md) |
| The `bd serve` HTTP surface | [engdocs/SERVE_RUNBOOK.md](engdocs/SERVE_RUNBOOK.md) |
| A new example under `examples/` | `examples/README.md` — give the example its own README and link it there |
| Scope questions | [engdocs/PROJECT_CHARTER.md](engdocs/PROJECT_CHARTER.md) |
| Planned work and priorities | [ROADMAP.md](ROADMAP.md) |
