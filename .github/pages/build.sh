#!/usr/bin/env bash
# Build the one-page GitHub Pages site from README.md.
#
# Usage: .github/pages/build.sh [output-directory]
#
# The README is converted to GitHub-flavored HTML with the pinned `marked`
# release and wrapped in the styled template next to this script. The result is
# a self-contained `index.html` plus `.nojekyll`.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${1:-"$root/_site"}"
template="$root/.github/pages/template.html"

if ! command -v npx >/dev/null 2>&1; then
  echo "npx (Node.js) is required to build the Pages site." >&2
  exit 1
fi

mkdir -p "$out"
body="$(mktemp)"
trap 'rm -f "$body"' EXIT

npx --yes marked@15.0.12 --gfm "$root/README.md" >"$body"

node - "$template" "$body" "$out/index.html" <<'NODE'
const fs = require("fs");
const [templatePath, bodyPath, outputPath] = process.argv.slice(2);
const template = fs.readFileSync(templatePath, "utf8");
const body = fs.readFileSync(bodyPath, "utf8");
if (!template.includes("{{CONTENT}}")) {
  throw new Error("template.html is missing the {{CONTENT}} placeholder");
}
fs.writeFileSync(outputPath, template.replace("{{CONTENT}}", body));
NODE

touch "$out/.nojekyll"
echo "Wrote $out/index.html"
