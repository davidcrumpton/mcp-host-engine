/// <reference path="../../types/mcphe.d.ts" />

/**
 * wikipedia_search.js — Enhanced Wikipedia plugin for mcphe
 *
 * Ports the full capability of the Python Wikipedia tool:
 *   - detail levels: brief / standard / full
 *   - disambiguation detection and handling
 *   - section-aware extraction (standard mode)
 *   - multi-language support (ISO 639-1 codes)
 *   - related article links
 *
 * goja runtime constraints observed throughout:
 *   - No async/await — all host.http.get calls are synchronous Go functions
 *   - Headers passed as a flat {key: value} object (not nested under "headers:")
 *   - No `this` inside call() — use `const self = module.exports` pattern if needed
 *   - host.http.get returns {status, headers, body} where body is a string
 */

const plugin = {
  name: "wikipedia_search",
  description: [
    "Search Wikipedia and return article content at three detail levels:",
    "'brief' (intro only), 'standard' (intro + section summaries, default),",
    "or 'full' (complete article). Supports 20+ languages via ISO 639-1 codes.",
    "Handles disambiguation pages and returns related article links."
  ].join(" "),
  version: "2.1.2",
  commit: "none",
  Tags: ["search", "utility"],
  annotations: {
    readOnlyHint:    true,
    destructiveHint: false,
    idempotentHint:  false,
    openWorldHint:   true,
  },

  inputSchema: {
    type: "object",
    properties: {
      query: {
        type: "string",
        description: "Topic, person, concept, or question to look up."
      },
      detail: {
        type: "string",
        enum: ["brief", "standard", "full"],
        default: "standard",
        description: [
          "'brief' = intro paragraph only.",
          "'standard' = intro + section summaries (default).",
          "'full' = complete article text."
        ].join(" ")
      },
      language: {
        type: "string",
        default: "en",
        description: "Wikipedia language edition as ISO 639-1 code (e.g. 'fr', 'de', 'es', 'ja'). Defaults to 'en'."
      },
      mode: {
        type: "string",
        enum: ["lookup", "search"],
        default: "lookup",
        description: "'lookup' fetches article content (default). 'search' returns a list of matching titles."
      }
    },
    required: ["query"]
  },

  call(params: Record<string, unknown>) {
    const query    = String(params.query || "").trim();
    const language = String(params.language || "en").trim().toLowerCase() || "en";
    const mode     = String(params.mode || "lookup").trim().toLowerCase();

    // Normalise detail with alias support
    const detailAliases = { intro: "brief", short: "brief", long: "full", all: "full" };
    let detail = String(params.detail || "standard").trim().toLowerCase();
    detail = detailAliases[detail] || detail;
    if (!["brief", "standard", "full"].includes(detail)) detail = "standard";

    if (!query) return { success: false, result: "query parameter is required." };

    try {
      if (mode === "search") {
        return searchMode(query, language);
      }
      return lookupMode(query, detail, language);
    } catch (err) {
      return { success: false, result: "Unexpected error: " + err.message };
    }
  }
};

// ─── Constants ───────────────────────────────────────────────────────────────

const USER_AGENT = "mcphe-wikipedia-plugin/2.0 (https://github.com/davidcrumpton/mcp-host-engine; mcphe)";

const LANG_NAMES = {
  en: "English", fr: "French", de: "German", es: "Spanish", it: "Italian",
  pt: "Portuguese", nl: "Dutch", ru: "Russian", ja: "Japanese", zh: "Chinese",
  ar: "Arabic", ko: "Korean", pl: "Polish", sv: "Swedish", fa: "Persian",
  tr: "Turkish", uk: "Ukrainian", vi: "Vietnamese", he: "Hebrew", id: "Indonesian"
};

const SKIP_SECTIONS = new Set([
  "see also", "references", "external links", "further reading",
  "notes", "bibliography", "citations", "footnotes", "sources"
]);

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

/**
 * Synchronous GET via the mcphe host API.
 * Headers must be a flat {key: stringValue} object — NOT nested.
 */
function httpGet(url: string, extraHeaders: { [key: string]: any }) {
  const headers = Object.assign({
    "User-Agent": USER_AGENT,
    "Accept": "application/json"
  }, extraHeaders || {});

  const response = host.http.get(url, headers);
  return response;
}

function apiUrl(lang: string, params: { [key: string]: any }) {
  const base = "https://" + lang + ".wikipedia.org/w/api.php";
  const parts = ["format=json", "utf8=1"];
  for (const k in params) {
    parts.push(encodeURIComponent(k) + "=" + encodeURIComponent(params[k]));
  }
  return base + "?" + parts.join("&");
}

function restSummaryUrl(lang: string, title: string) {
  return "https://" + lang + ".wikipedia.org/api/rest_v1/page/summary/" +
         encodeURIComponent(title.replace(/ /g, "_"));
}

function pageUrl(title: string, lang: string) {
  return "https://" + lang + ".wikipedia.org/wiki/" +
         encodeURIComponent(title.replace(/ /g, "_"));
}

// ─── Text utilities ───────────────────────────────────────────────────────────

function cleanText(text: string) {
  if (!text) return "";
  text = text.replace(/\n{3,}/g, "\n\n");
  text = text.replace(/[ \t]{2,}/g, " ");
  text = text.replace(/\[\d+\]/g, "");
  text = text.replace(/\[note \d+\]/g, "");
  return text.trim();
}

function stripHtml(text: string) {
  return (text || "").replace(/<[^>]+>/g, "");
}

function stripWikitext(text: string) {
  if (!text) return "";

  // Remove templates {{...}} — iterate until stable (handles light nesting)
  let prev;
  let iterations = 0;
  do {
    prev = text;
    text = text.replace(/\{\{[^{}]*\}\}/g, "");
    iterations++;
  } while (prev !== text && iterations < 10);

  // Remove [[File:...]] / [[Image:...]]
  text = text.replace(/\[\[(?:File|Image|Media):[^\]]*\]\]/gi, "");

  // [[link|label]] → label, [[link]] → link
  text = text.replace(/\[\[(?:[^|\]]*\|)?([^\]]+)\]\]/g, "$1");

  // External links [http://... label] → label, bare [http://...] → ''
  text = text.replace(/\[https?:\/\/\S+\s+([^\]]+)\]/g, "$1");
  text = text.replace(/\[https?:\/\/\S+\]/g, "");

  // Bold/italic
  text = text.replace(/'{2,3}/g, "");

  // Section headers
  text = text.replace(/={2,}[^=\n]+=*={2,}/g, "");

  // <ref> tags and HTML
  text = text.replace(/<ref[^>]*\/?>[\s\S]*?<\/ref>/gi, "");
  text = text.replace(/<[^>]+>/g, "");

  return cleanText(text);
}

function truncate(text: string, maxChars: number) {
  if (!text || text.length <= maxChars) return { text: text || "", wasCut: false };
  let cut = text.slice(0, maxChars);
  const lastPara = cut.lastIndexOf("\n\n");
  if (lastPara > maxChars * 0.7) cut = cut.slice(0, lastPara);
  return { text: cut, wasCut: true };
}

function isDisambiguation(title: string, extract: string) {
  const lowt = (title || "").toLowerCase();
  const lowe = (extract || "").toLowerCase();
  return (
    lowt.includes("(disambiguation)") ||
    lowe.includes("may refer to:") ||
    lowe.includes("may refer to\n") ||
    (lowe.trimLeft().startsWith("this article is about") &&
      (lowe.includes("for other uses") || lowe.includes("see also")))
  );
}

// ─── Wikipedia API calls (all synchronous) ───────────────────────────────────

function searchTitles(query: string, lang: string, limit: number) {
  limit = limit || 6;
  const url = apiUrl(lang, {
    action: "query",
    list: "search",
    srsearch: query,
    srlimit: limit,
    srprop: "snippet|titlesnippet"
  });
  const resp = host.http.get(url);
  if (resp.status !== 200) return [];
  try {
    const data = JSON.parse(resp.body);
    return (data.query && data.query.search) || [];
  } catch (e) {
    return [];
  }
}

function fetchRestSummary(lang: string, title: string) {
  const url = restSummaryUrl(lang, title);
  const resp = host.http.get(url);
  if (resp.status !== 200) return null;
  try {
    return JSON.parse(resp.body);
  } catch (e) {
    return null;
  }
}

/**
 * Fetch plain-text extract via MediaWiki action API.
 * introOnly=true returns only the lead section.
 */
function fetchExtract(title: string, lang: string, introOnly: boolean) {
  const params = {
    action: "query",
    prop: "extracts",
    titles: title,
    explaintext: 1,
    redirects: 1
  };
  if (introOnly) params.exintro = 1;

  const url = apiUrl(lang, params);
  const resp = host.http.get(url);
  if (resp.status !== 200) return "";
  try {
    const data = JSON.parse(resp.body);
    const pages = (data.query && data.query.pages) || {};
    const page = pages[Object.keys(pages)[0]] || {};
    return cleanText(page.extract || "");
  } catch (e) {
    return "";
  }
}

function fetchSections(title: string, lang: string) {
  const url = apiUrl(lang, {
    action: "parse",
    page: title,
    prop: "sections",
    redirects: 1
  });
  const resp = host.http.get(url);
  if (resp.status !== 200) return [];
  try {
    const data = JSON.parse(resp.body);
    return (data.parse && data.parse.sections) || [];
  } catch (e) {
    return [];
  }
}

function fetchSectionText(title: string, lang: string, sectionIndex: string) {
  const url = apiUrl(lang, {
    action: "parse",
    page: title,
    prop: "wikitext",
    section: sectionIndex,
    redirects: 1,
    disableeditsection: 1
  });
  const resp = host.http.get(url);
  if (resp.status !== 200) return "";
  try {
    const data = JSON.parse(resp.body);
    const wikitext = (data.parse && data.parse.wikitext && data.parse.wikitext["*"]) || "";
    return stripWikitext(wikitext);
  } catch (e) {
    return "";
  }
}

function fetchLinks(title: string, lang: string, limit: number) {
  limit = limit || 6;
  const url = apiUrl(lang, {
    action: "query",
    prop: "links",
    titles: title,
    pllimit: limit,
    plnamespace: 0
  });
  const resp = host.http.get(url);
  if (resp.status !== 200) return [];
  try {
    const data = JSON.parse(resp.body);
    const pages = (data.query && data.query.pages) || {};
    const page = pages[Object.keys(pages)[0]] || {};
    return (page.links || []).map(function(l: any) { return l.title; });
  } catch (e) {
    return [];
  }
}

// ─── Disambiguation handler ───────────────────────────────────────────────────

function handleDisambiguation(title: string, lang: string, searchResults: string[] | null) {
  const lines = [
    "\"" + title + "\" is a disambiguation page — multiple articles match this title.\n",
    "Here are the closest results:\n"
  ];
  const results = (searchResults || []).slice(0, 6);
  for (let i = 0; i < results.length; i++) {
    const r: any = results[i];
    let snippet = stripHtml(r.snippet || "").trim().replace(/\s+/g, " ");
    if (snippet.length > 120) snippet = snippet.slice(0, 120) + "…";
    const url = pageUrl(r.title, lang);
    lines.push((i + 1) + ". " + r.title + "\n   " + snippet + "\n   " + url);
  }
  return { success: false, disambiguation: true, result: lines.join("\n") };
}

// ─── Search mode ──────────────────────────────────────────────────────────────

function searchMode(query: string, lang: any) {
  const results = searchTitles(query, lang, 8);
  if (!results.length) {
    const label = lang !== "en" ? " in " + (LANG_NAMES[lang] || lang) + " Wikipedia." : ".";
    return { success: false, result: "No Wikipedia results found for \"" + query + "\"" + label };
  }

  const langLabel = lang !== "en" ? " (" + (LANG_NAMES[lang] || lang) + " Wikipedia)" : "";
  const lines = ["Wikipedia search results for \"" + query + "\"" + langLabel + "\n"];

  for (let i = 0; i < results.length; i++) {
    const r = results[i];
    let snippet = stripHtml(r.snippet || "").trim().replace(/\s+/g, " ");
    if (snippet.length > 150) snippet = snippet.slice(0, 150) + "…";
    const url = pageUrl(r.title, lang);
    lines.push((i + 1) + ". " + r.title + "\n   " + snippet + "\n   " + url);
  }

  return { success: true, result: lines.join("\n") };
}

// ─── Lookup mode ──────────────────────────────────────────────────────────────

function lookupMode(query: string, detail: string, lang: string) {
  // Step 1: find best matching article
  const searchResults = searchTitles(query, lang, 6);
  if (!searchResults.length) {
    const label = lang !== "en" ? " in " + (LANG_NAMES[lang] || lang) + " Wikipedia." : ".";
    return { success: false, result: "No Wikipedia articles found for \"" + query + "\"" + label };
  }

  const title = searchResults[0].title;

  // Step 2: REST summary for canonical title, description, URL, last-modified
  let canonicalTitle = title;
  let description    = "";
  let restExtract    = "";
  let articleUrl     = pageUrl(title, lang);
  let lastModified   = "";

  const rest = fetchRestSummary(lang, title);
  if (rest) {
    if (rest.type === "disambiguation") {
      return handleDisambiguation(title, lang, searchResults);
    }
    canonicalTitle = rest.title || title;
    description    = rest.description || "";
    restExtract    = rest.extract || "";
    lastModified   = rest.timestamp ? rest.timestamp.slice(0, 10) : "";
    if (rest.content_urls && rest.content_urls.desktop && rest.content_urls.desktop.page) {
      articleUrl = rest.content_urls.desktop.page;
    }
  }

  // Step 3: Build output
  const parts = [];

  // Header
  const headerLines = ["## " + canonicalTitle];
  if (description) headerLines.push("_" + description + "_");
  if (lastModified) headerLines.push("Last updated: " + lastModified);
  headerLines.push(articleUrl);
  parts.push(headerLines.join("\n"));

  // ── brief ─────────────────────────────────────────────────────────────────
  if (detail === "brief") {
    const intro = restExtract || fetchExtract(canonicalTitle, lang, true);
    parts.push(intro || "(No summary available.)");
  }

  // ── standard ──────────────────────────────────────────────────────────────
  else if (detail === "standard") {
    const intro = fetchExtract(canonicalTitle, lang, true);
    if (intro) {
      const t = truncate(intro, 2000);
      parts.push("### Introduction\n" + t.text);
    }

    try {
      const allSections = fetchSections(canonicalTitle, lang);

      // Top-level sections only, skip boilerplate
      const keySections = allSections.filter(function(s) {
        return s.toclevel === 1 && !SKIP_SECTIONS.has((s.line || "").toLowerCase());
      }).slice(0, 6);

      if (keySections.length) {
        const toc = keySections.map(function(s) { return "  - " + s.line; }).join("\n");
        parts.push("### Contents\n" + toc);

        const summaries = [];
        const cap = Math.min(keySections.length, 4);
        for (let i = 0; i < cap; i++) {
          const s    = keySections[i];
          const idx  = parseInt(s.index || "0", 10);
          if (idx === 0) continue;

          const secText = fetchSectionText(canonicalTitle, lang, idx);
          if (!secText) continue;

          // First paragraph, capped at 500 chars
          const firstPara = secText.split("\n\n")[0].trim();
          const t = truncate(firstPara, 500);
          if (t.text) summaries.push("**" + s.line + "**\n" + t.text);
        }

        if (summaries.length) parts.push(summaries.join("\n\n---\n\n"));
      }
    } catch (e) {
      // Graceful fallback to plain extract
      const full = fetchExtract(canonicalTitle, lang, false);
      const t = truncate(full, 4000);
      parts.push(t.text);
      if (t.wasCut) parts.push("…article continues at " + articleUrl);
    }
  }

  // ── full ──────────────────────────────────────────────────────────────────
  else {
    const fullText = fetchExtract(canonicalTitle, lang, false);
    if (isDisambiguation(canonicalTitle, fullText)) {
      return handleDisambiguation(canonicalTitle, lang, searchResults);
    }
    const t = truncate(fullText, 9000);
    parts.push(t.text);
    if (t.wasCut) parts.push("…article truncated. Full article: " + articleUrl);
  }

  // Step 4: Related articles
  try {
    const links = fetchLinks(canonicalTitle, lang, 6);
    if (links.length) {
      parts.push("Related topics: " + links.slice(0, 6).join(", "));
    }
  } catch (e) { /* non-fatal */ }

  // Disambiguation safety check for brief/standard
  if (detail !== "full" && isDisambiguation(canonicalTitle, restExtract)) {
    return handleDisambiguation(canonicalTitle, lang, searchResults);
  }

  return {
    success: true,
    title:   canonicalTitle,
    url:     articleUrl,
    result:  parts.join("\n\n")
  };
}

module.exports = plugin;
