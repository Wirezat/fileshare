importScripts("/static/highlight.min.js");

/* Returns the highlight.js language for a file name, or "" when it has none.
   @returns {string} */
function languageFor(name) {
    const lower = name.toLowerCase();
    const ext = lower.includes(".") ? lower.slice(lower.lastIndexOf(".") + 1) : "";
    for (const key of [ext, lower]) {
        if (key && hljs.getLanguage(key)) return key;
    }
    return "";
}

self.onmessage = ({ data: { id, name, text } }) => {
    const language = languageFor(name);
    const html = language ? hljs.highlight(text, { language, ignoreIllegals: true }).value : null;
    self.postMessage({ id, html });
};
