#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="${1:?Usage: bootstrap.sh /absolute/path/to/langfuse-agenttrace}"
if [[ ! -f "$target/web/package.json" ]]; then
  printf '%s\n' "Target must be the Langfuse source repository." >&2
  exit 1
fi
if ! rg -q 'browserRuns: browserRunsRouter' "$target/web/src/server/api/root.ts"; then
  printf '%s\n' "Install the existing AgentTrace first-loop router integration first (see README)." >&2
  exit 1
fi
if git -C "$target" apply --check "$here/wiring.patch" 2>/dev/null; then
  git -C "$target" apply "$here/wiring.patch"
elif ! git -C "$target" apply --reverse --check "$here/wiring.patch" 2>/dev/null; then
  printf '%s\n' "Wiring patch conflicts with local edits. Apply its two scoped changes manually before retrying." >&2
  exit 1
fi
# Explicit product-source overlay only; no env values, build output or repo-wide patch.
cp -R "$here/source/." "$target/"
printf '%s\n' "Management source installed. Existing services were not changed."
