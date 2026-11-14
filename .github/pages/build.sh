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
  local layout_start="$7" sidebar="$8" layout_end="$9" body_class="${10:-}"
  local body
  body="$(mktemp)"
  npx --yes marked@15.0.12 --gfm "$src" >"$body"
  node - "$template" "$body" "$dst" "$title" "$description" "$canonical" "$relroot" \
    "$layout_start" "$sidebar" "$layout_end" "$body_class" <<'NODE'
const fs = require("fs");
const [
  templatePath, bodyPath, outputPath, title, description, canonical, root,
  layoutStart, sidebar, layoutEnd, bodyClass,
] = process.argv.slice(2);
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

const toc = [];
const seen = new Map();
body = body.replace(/<h2>([\s\S]*?)<\/h2>/g, (match, inner) => {
  const text = inner.replace(/<[^>]*>/g, "").replace(/\s+/g, " ").trim();
  let id = text.toLowerCase()
    .replace(/&[a-z]+;/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
  if (!id) id = "section";
  const n = (seen.get(id) || 0) + 1;
  seen.set(id, n);
  if (n > 1) id += "-" + n;
  toc.push({ id, text });
  return `<h2 id="${id}">${inner}</h2>`;
});
const tocHtml = toc.length
  ? `<nav class="toc" aria-label="On this page"><p class="toc-title">On this page</p><ul>` +
    toc.map((t) => `<li><a href="#${t.id}">${t.text}</a></li>`).join("") +
    `</ul></nav>`
  : "";

const values = {
  "{{TITLE}}": title,
  "{{DESCRIPTION}}": description,
  "{{CANONICAL}}": canonical,
  "{{ROOT}}": root,
  "{{BODY_CLASS}}": bodyClass,
  "{{LAYOUT_START}}": layoutStart,
  "{{SIDEBAR}}": sidebar,
  "{{LAYOUT_END}}": layoutEnd,
  "{{TOC}}": tocHtml,
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

html_escape() {
  sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g'
}

docs_order=(index install cli keys automation security sites development)

sidebar_for() {
  local current="$1" name title active
  printf '<aside class="docs-sidebar" id="sidebar" aria-label="Documentation">'
  printf '<h2>Documentation</h2><ul>'
  for name in "${docs_order[@]}"; do
    title="$(read_title "$root/docs/$name.md" "$name")"
    title="$(printf '%s' "$title" | html_escape)"
    active=""
    if [ "$name" = "$current" ]; then
      active=' class="active" aria-current="page"'
    fi
    printf '<li><a%s href="%s.html">%s</a></li>' "$active" "$name" "$title"
  done
  printf '</ul></aside>'
}

readme_title="$(read_title "$root/README.md" "MOB³ — a small terminal web browser in Go")"
render_page "$root/README.md" "$out/index.html" "$readme_title" \
  "MOB³ is a small terminal web browser written in Go. It renders HTML, a practical CSS subset and images in 24-bit color." \
  "https://mob3.net/" "." "" "" "" ""

for src in "$root"/docs/*.md; do
  [ -e "$src" ] || continue
  name="$(basename "$src" .md)"
  title="$(read_title "$src" "MOB³ documentation")"
  render_page "$src" "$out/docs/$name.html" "$title" \
    "MOB³ documentation — $title" \
    "https://mob3.net/docs/$name.html" ".." \
    '<div class="docs-layout">' "$(sidebar_for "$name")" '</div>' "docs-page"
done

cp "$root/.github/pages/search.js" "$out/search.js"

node - "$out" <<'NODE'
const fs = require("fs");
const path = require("path");
const out = process.argv[2];

function files(dir) {
  const result = [];
  for (const name of fs.readdirSync(dir)) {
    const full = path.join(dir, name);
    const stat = fs.statSync(full);
    if (stat.isDirectory()) result.push(...files(full));
    else if (name.endsWith(".html")) result.push(full);
  }
  return result;
}

function strip(html) {
  return String(html)
    .replace(/<script[\s\S]*?<\/script>/gi, " ")
    .replace(/<style[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/\s+/g, " ")
    .trim();
}

const entries = [];
for (const file of files(out)) {
  if (path.basename(file) === "search-index.js") continue;
  const rel = path.relative(out, file).split(path.sep).join("/");
  const html = fs.readFileSync(file, "utf8");
  const title = strip((html.match(/<title>([\s\S]*?)<\/title>/i) || [])[1] || rel);
  const main = (html.match(/<main[^>]*>([\s\S]*?)<\/main>/i) || [])[1] || html;
  const body = main.replace(/<nav class="toc"[\s\S]*?<\/nav>/i, " ");
  const headings = [...body.matchAll(/<h2 id="([^"]+)"[^>]*>([\s\S]*?)<\/h2>/gi)].map(
    (m) => ({ id: m[1], text: strip(m[2]) }),
  );
  entries.push({
    path: rel,
    title,
    text: strip(body).slice(0, 50000),
    headings,
  });
}
entries.sort((a, b) => {
  if (a.path === "index.html") return -1;
  if (b.path === "index.html") return 1;
  return a.path.localeCompare(b.path);
});
fs.writeFileSync(
  path.join(out, "search-index.js"),
  "window.MOB3_SEARCH = " + JSON.stringify(entries) + ";\n",
);
NODE

printf 'mob3.net\n' >"$out/CNAME"
printf '/bin/\n/_site/\n' >"$out/.gitignore"
touch "$out/.nojekyll"
echo "Wrote $out/index.html, $out/docs/*.html, search index and CNAME"
