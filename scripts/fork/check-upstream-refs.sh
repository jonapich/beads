#!/usr/bin/env bash
#
# check-upstream-refs.sh - Fail if this tree could fetch from, send data to, or
# tell anyone to install from infrastructure the original project controls.
# Run it through `docker buildx bake guard`.
#
# History and fixtures are exempt: they name the original project but fetch
# nothing. Links to its issues and pull requests are allowed for the same reason.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

readonly owners='(gastownhall|steveyegge)'
readonly patterns=(
  "raw\.githubusercontent\.com/${owners}/"
  "api\.github\.com/repos/${owners}/"
  "github\.com/${owners}/beads/(releases|archive)"
  "github\.com/gastownhall/gascity"
  "git@github\.com:${owners}/"
  "git clone [^ ]*${owners}/"
  "gastownhall-eventsapi\.com"
  "\.ops\.gascity\.com"
  "beads\.gascity\.com"
  "ghcr\.io/${owners}"
  "github:${owners}/"
  "marketplace add ${owners}/"
  "(--repo[ =]|-R )${owners}/"
  "go install [^ ]*github\.com/${owners}/"
  "(pip|pipx|uv tool) install[^|]*beads-mcp"
  "uvx beads-mcp"
  "(npm|bun) (install|update)[^|]*@beads/"
  "brew (install|upgrade) beads"
)

joined="$(IFS='|'; printf '%s' "${patterns[*]}")"
status=0

if grep -rInE "$joined" . \
  --exclude-dir=.git \
  --exclude-dir=dist \
  --exclude-dir=fork \
  --exclude-dir=engdocs \
  --exclude-dir=release-gates \
  --exclude-dir=testdata \
  --exclude='*_test.go' \
  --exclude=CHANGELOG.md \
  --exclude=NEWSLETTER.md \
  --exclude=ARTICLES.md \
  --exclude=info.go; then
  printf '\nERROR: the lines above reference the original project'\''s infrastructure.\n' >&2
  status=1
fi

# A removed path coming back is how a new workflow or build input would arrive.
removed="$(tr -d '\r' < scripts/fork/removed-paths.txt | grep -vE '^[[:space:]]*(#|$)' | paste -sd '|' -)"
if find . -type f -not -path './.git/*' -not -path './dist/*' | sed 's|^\./||' | grep -E "$removed"; then
  printf '\nERROR: the files above are listed in scripts/fork/removed-paths.txt.\n' >&2
  status=1
fi

if ((status == 0)); then
  printf 'OK: no references to the original project'\''s infrastructure.\n'
fi
exit "$status"
