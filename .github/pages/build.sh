#!/usr/bin/env bash
# Build the GitHub Pages site from README.md and docs/*.md.
#
# Usage: .github/pages/build.sh [output-directory]
#
# Each page is converted to GitHub-flavored HTML with the pinned `marked`
# release and wrapped in the styled template next to this script. Relative
# .md links are rewritten to .html so the same Markdown works on GitHub and on
# the site.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
out="${1:-"$root/_site"}"
template="$root/.github/pages/template.html"

if ! command -v npx >/dev/null 2>&1; then
  echo "npx (Node.js) is required to build the Pages site." >&2
  exit 1
fi

mkdir -p "$out/docs"

render_page() {
  local src="$1" dst="$2" title="$3" description="$4" canonical="$5" relroot="$6"
  local body
  body="$(mktemp)"
  npx --yes marked@15.0.12 --gfm "$src" >"$body"
  node - "$template" "$body" "$dst" "$title" "$description" "$canonical" "$relroot" <<'NODE'
const fs = require("fs");
const [templatePath, bodyPath, outputPath, title, description, canonical, root] =
  process.argv.slice(2);
const template = fs.readFileSync(templatePath, "utf8");
let body = fs.readFileSync(bodyPath, "utf8");

// Keep Markdown links working on both GitHub (.md) and Pages (.html).
body = body.replace(/href="([^"]+)"/g, (match, href) => {
  if (/^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(href)) return match;
  const hash = href.indexOf("#");
  const path = hash < 0 ? href : href.slice(0, hash);
  const suffix = hash < 0 ? "" : href.slice(hash);
  if (!path.endsWith(".md")) return match;
  const target =
    path === "README.md" || path.endsWith("/README.md")
      ? path.slice(0, -"README.md".length) + "index.html"
      : path.slice(0, -3) + ".html";
  return `href="${target}${suffix}"`;
});

const values = {
  "{{TITLE}}": title,
  "{{DESCRIPTION}}": description,
  "{{CANONICAL}}": canonical,
  "{{ROOT}}": root,
  "{{CONTENT}}": body,
};
let html = template;
for (const [key, value] of Object.entries(values)) {
  html = html.split(key).join(value);
}
if (html.includes("{{")) throw new Error("unreplaced template placeholder");
fs.writeFileSync(outputPath, html);
NODE
  rm -f "$body"
}

read_title() {
  local src="$1" fallback="$2" title
  title="$(grep -m1 '^# ' "$src" | sed 's/^# //' || true)"
  if [ -z "$title" ]; then title="$fallback"; fi
  printf '%s' "$title"
}

readme_title="$(read_title "$root/README.md" "MOB³ — a small terminal web browser in Go")"
render_page "$root/README.md" "$out/index.html" "$readme_title" \
  "MOB³ is a small terminal web browser written in Go. It renders HTML, a practical CSS subset and images in 24-bit color." \
  "https://mob3.net/" "."

for src in "$root"/docs/*.md; do
  [ -e "$src" ] || continue
  name="$(basename "$src" .md)"
  title="$(read_title "$src" "MOB³ documentation")"
  render_page "$src" "$out/docs/$name.html" "$title" \
    "MOB³ documentation — $title" \
    "https://mob3.net/docs/$name.html" ".."
done

printf 'mob3.net\n' >"$out/CNAME"
printf '/bin/\n/_site/\n' >"$out/.gitignore"
touch "$out/.nojekyll"
echo "Wrote $out/index.html and $out/docs/*.html, plus CNAME and .gitignore"
