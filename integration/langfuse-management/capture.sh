#!/usr/bin/env bash
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source="${1:-/Users/bytedance/project/langfuse-agenttrace}"
paths=(
  web/src/features/browser-runs
  web/src/__tests__/server/unit/browser-runs
  web/src/pages/api/agenttrace
  "web/src/pages/project/[projectId]/browser-runs"
  web/src/__e2e__/browser-runs.spec.ts
)
for path in "${paths[@]}"; do
  mkdir -p "$here/source/$(dirname "$path")"
  cp -R "$source/$path" "$here/source/$(dirname "$path")/"
done
git -C "$source" diff -- web/src/env.mjs web/src/components/layouts/routes.tsx > "$here/wiring.patch"
find "$here/source" -type f -exec shasum -a 256 {} + > "$here/source.sha256"
printf '%s\n' "Captured scoped product source and tests; no secrets or build artifacts."
