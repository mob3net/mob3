(function () {
  "use strict";

  var input = document.getElementById("search");
  var box = document.getElementById("search-results");
  if (!input || !box) return;

  var index = window.MOB3_SEARCH || [];
  var root = document.documentElement.getAttribute("data-root") || ".";
  var prefix = root === "." ? "./" : "../";
  var results = [];
  var active = -1;

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function hide() {
    box.classList.add("hidden");
    active = -1;
  }

  function linkFor(entry, heading) {
    return prefix + entry.path + (heading ? "#" + heading.id : "");
  }

  function snippet(text, tokens) {
    var lower = text.toLowerCase();
    var at = -1;
    for (var i = 0; i < tokens.length; i++) {
      var j = lower.indexOf(tokens[i]);
      if (j >= 0 && (at < 0 || j < at)) at = j;
    }
    if (at < 0) at = 0;
    var start = Math.max(0, at - 48);
    var end = Math.min(text.length, start + 150);
    return (start > 0 ? "…" : "") + text.slice(start, end) + (end < text.length ? "…" : "");
  }

  function run() {
    var q = input.value.trim().toLowerCase();
    if (!q) {
      results = [];
      hide();
      return;
    }
    var tokens = q.split(/\s+/).filter(Boolean);
    var found = [];
    for (var i = 0; i < index.length; i++) {
      var entry = index[i];
      var title = entry.title.toLowerCase();
      var text = entry.text.toLowerCase();
      var headings = entry.headings || [];
      var headingText = headings.map(function (h) { return h.text.toLowerCase(); }).join(" ");
      var score = 0;
      var ok = true;
      for (var t = 0; t < tokens.length; t++) {
        var token = tokens[t];
        var inTitle = title.indexOf(token) >= 0;
        var inHeading = headingText.indexOf(token) >= 0;
        var inText = text.indexOf(token) >= 0;
        if (!inTitle && !inHeading && !inText) {
          ok = false;
          break;
        }
        score += (inTitle ? 20 : 0) + (inHeading ? 10 : 0) + (inText ? 1 : 0);
      }
      if (!ok) continue;
      var heading = null;
      for (var h = 0; h < headings.length; h++) {
        if (headings[h].text.toLowerCase().indexOf(tokens[0]) >= 0) {
          heading = headings[h];
          break;
        }
      }
      found.push({
        title: entry.title,
        href: linkFor(entry, heading),
        snippet: snippet(entry.text || entry.title, tokens),
        score: score,
      });
    }
    found.sort(function (a, b) {
      return b.score - a.score || a.title.length - b.title.length;
    });
    results = found.slice(0, 20);
    active = results.length ? 0 : -1;
    render();
  }

  function render() {
    box.classList.remove("hidden");
    if (!results.length) {
      box.innerHTML = '<p class="empty">No matches.</p>';
      return;
    }
    box.innerHTML = results
      .map(function (r, i) {
        return (
          '<a role="option" aria-selected="' + (i === active ? "true" : "false") +
          '" class="' + (i === active ? "active" : "") + '" href="' +
          escapeHtml(r.href) + '"><span class="page">' +
          escapeHtml(r.title) + '</span><span class="hint">' +
          escapeHtml(r.snippet) + "</span></a>"
        );
      })
      .join("");
  }

  input.addEventListener("input", run);
  input.addEventListener("focus", function () {
    if (input.value.trim()) run();
  });
  input.addEventListener("keydown", function (event) {
    if (box.classList.contains("hidden")) return;
    if (event.key === "ArrowDown" && results.length) {
      event.preventDefault();
      active = (active + 1) % results.length;
      render();
    } else if (event.key === "ArrowUp" && results.length) {
      event.preventDefault();
      active = (active - 1 + results.length) % results.length;
      render();
    } else if (event.key === "Enter" && active >= 0 && results[active]) {
      event.preventDefault();
      window.location.href = results[active].href;
    } else if (event.key === "Escape") {
      event.preventDefault();
      hide();
      input.blur();
    }
  });
  document.addEventListener("click", function (event) {
    if (!box.contains(event.target) && event.target !== input) hide();
  });
})();
