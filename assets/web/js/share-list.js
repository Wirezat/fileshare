// ── Listing ───────────────────────────────────────────
const listing = {
    entries: (() => {
        try { return JSON.parse($("entries")?.textContent || "[]") ?? []; } catch { return []; }
    })(),
    visible: [],
};

/* Returns the entries whose name contains query (trimmed, case-insensitive), hiding dotfiles unless showDotfiles.
   @returns {object[]} the matching entries in listing order */
function filterEntries(entries, query, showDotfiles) {
    const q = query.trim().toLowerCase();
    return entries.filter(e => (showDotfiles || !e.name.startsWith(".")) && (!q || e.name.toLowerCase().includes(q)));
}

/* Returns the entry shown by the card or row around el, or null outside the listing.
   @returns {object | null} */
function entryAt(el) {
    const node = el.closest?.("[data-index]");
    return node ? listing.visible[Number(node.dataset.index)] ?? null : null;
}

/* Builds the link that opens entry, or downloads it when the entry is marked dl.
   @returns {HTMLAnchorElement} */
function entryLink(entry, className) {
    const a = document.createElement("a");
    if (className) a.className = className;
    a.href = entry.dl ? entry.href + "?dl=1" : entry.href;
    if (entry.dl) a.download = "";
    return a;
}

/* Builds the selection checkbox of entry, checked when its name is selected.
   @returns {HTMLInputElement} */
function selectBox(entry, className) {
    const box = Object.assign(document.createElement("input"), { type: "checkbox", className, checked: selected.has(entry.name) });
    box.setAttribute("aria-label", t("share.list.select").replace("{name}", entry.name));
    return box;
}

/* Builds the card of entry at position index of listing.visible and registers files for their preview.
   @returns {HTMLLIElement} */
function cardNode(entry, index) {
    const card = Object.assign(document.createElement("li"), { className: "file-card" });
    card.dataset.index = String(index);
    const preview = entry.dir ? entryLink(entry) : document.createElement("div");
    preview.className = "card-preview card-preview-icon";
    preview.appendChild(Object.assign(document.createElement("span"), { className: "card-icon", textContent: entry.icon }));
    const name = Object.assign(document.createElement("div"), { className: "card-name", title: entry.name });
    const link = entryLink(entry);
    link.textContent = entry.dir ? entry.name + "/" : entry.name;
    const size = Object.assign(document.createElement("span"), { className: "card-size" });
    if (!entry.dir) size.textContent = formatBytes(entry.size);
    name.append(link, size);
    card.append(selectBox(entry, "select-box card-select"), preview, name);
    if (!entry.dir) {
        const dl = Object.assign(document.createElement("a"), { className: "card-download", href: entry.href + "?dl=1", download: "", title: t("share.lightbox.download") });
        dl.setAttribute("aria-label", dl.title);
        dl.appendChild(svgIcon("M12 4v11m0 0l-5-5m5 5l5-5M5 20h14"));
        card.appendChild(dl);
        observePreview(card);
    }
    return card;
}

/* Builds the table row of entry at position index of listing.visible.
   @returns {HTMLTableRowElement} */
function rowNode(entry, index) {
    const row = Object.assign(document.createElement("tr"), { className: "file-row" });
    row.dataset.index = String(index);
    const cell = (className, ...children) => {
        const td = Object.assign(document.createElement("td"), { className });
        td.append(...children);
        return td;
    };
    const link = entryLink(entry);
    link.textContent = entry.dir ? entry.name + "/" : entry.name;
    const dl = [];
    if (!entry.dir) {
        dl.push(Object.assign(document.createElement("a"), {
            className: "row-download", href: entry.href + "?dl=1", download: "", title: t("share.lightbox.download"), textContent: "⭳",
        }));
    }
    row.append(
        cell("row-select col-narrow", selectBox(entry, "select-box")),
        cell("row-icon", entry.icon),
        cell("row-name", link),
        cell("row-size col-narrow", entry.dir ? "" : formatBytes(entry.size)),
        cell("row-dl col-narrow", ...dl),
    );
    return row;
}

/* Renders listing.visible into the grid or the table, whichever view is shown, and empties the other. */
function renderList() {
    const grid = $q(".file-grid"), body = $q(".file-table tbody");
    if (!grid || !body) return;
    const table = $("file-list").classList.contains("view-table");
    const make = table ? rowNode : cardNode;
    const frag = document.createDocumentFragment();
    listing.visible.forEach((e, i) => frag.appendChild(make(e, i)));
    (table ? body : grid).replaceChildren(frag);
    (table ? grid : body).replaceChildren();
    const empty = $("filter-empty");
    if (empty) empty.hidden = listing.visible.length > 0;
}
