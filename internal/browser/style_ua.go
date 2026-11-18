package browser

// UA stylesheet: base document styling expressed in the same CSS subset the
// engine understands.
const uaStylesheet = `
html { display:block; color:#1f2328; font-size:16px; line-height:1; }
body { display:block; margin:8px; padding:0; color:#1f2328; }
head, script, style, title, meta, link, base, template, param, source, track { display:none; }
[hidden] { display:none; }
div, p, h1, h2, h3, h4, h5, h6, ul, ol, li, dl, dt, dd, blockquote, pre,
table, thead, tbody, tfoot, tr, td, th, form, fieldset, legend, figure,
figcaption, header, footer, nav, main, section, article, aside, address,
hr, center, details, summary, hgroup, search { display:block; }
dialog { display:none; }
dialog[open] { display:block; }
p { margin:8px 0; }
h1 { font-size:32px; font-weight:bold; margin:8px 0; }
h2 { font-size:24px; font-weight:bold; margin:8px 0; }
h3 { font-size:19px; font-weight:bold; margin:8px 0 4px; }
h4, h5, h6 { font-size:16px; font-weight:bold; margin:8px 0 4px; }
b, strong, th { font-weight:bold; }
i, em, cite, var, q { font-style:italic; }
u, ins { text-decoration:underline; }
s, del, strike { text-decoration:line-through; }
a { display:inline; color:#0645ad; text-decoration:underline; }
pre, code, kbd, samp, tt { font-family:monospace; }
pre { white-space:pre; margin:8px 0; padding:4px; background:#f6f8fa; }
code { background:#f6f8fa; }
blockquote { margin:8px 12px; border-left:3px solid #d0d7de; padding-left:8px; color:#57606a; }
ul, ol { margin:8px 0; padding-left:20px; }
ul { list-style-type:disc; }
ol { list-style-type:decimal; }
li { display:list-item; margin:2px 0; }
hr { display:block; border-top:1px solid #d0d7de; margin:8px 0; height:0; }
table { display:table; margin:4px 0; border-color:#d0d7de; }
thead, tbody, tfoot { display:block; }
tr { display:table-row; }
td, th { display:table-cell; padding:2px 4px; border:1px solid #d0d7de; vertical-align:middle; }
th { text-align:center; background:#f3f4f6; }
caption { display:block; font-weight:bold; text-align:center; }
img { display:inline-block; }
input, select, textarea, button { display:inline-block; border:1px solid #8c959f; padding:0 1px; background:#f6f8fa; color:#1f2328; }
small { font-size:12px; }
big { font-size:20px; }
sub, sup { font-size:12px; }
center { text-align:center; }
`

// uaRules is parsed once; rebuilding layout must not re-parse the UA sheet.
var uaRules = ParseStylesheet(uaStylesheet)
