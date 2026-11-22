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

const CARD_RATIO_MIN = 80;
const CARD_RATIO_MAX_VH = 0.7;

/* Shows the preview box of a card in the aspect ratio ar (width / height) when the natural card format is on. */
function applyCardRatio(box, ar) {
    box.style.setProperty("--ar", String(ar));
    box.classList.add("has-ratio");
}

/* Builds the card of entry at position index of listing.visible and registers files for their preview.
   @returns {HTMLLIElement} */
function cardNode(entry, index) {
    const card = Object.assign(document.createElement("li"), { className: "file-card" });
    card.dataset.index = String(index);
    const preview = entry.dir ? entryLink(entry) : document.createElement("div");
    preview.className = "card-preview card-preview-icon";
    if (!entry.dir && entry.ar > 0) applyCardRatio(preview, entry.ar);
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

const LIST_BUFFER_ROWS = 3;
const listView = { nodes: new Map(), tops: null, columns: 1, width: 0, viewHeight: 0, stale: false, scheduled: false, top: null, bottom: null };

/* Returns the offset of each row of count items laid out in columns from the list top, followed by the end of
   the last row; heightOf(i) is the height of item i, a row is as high as its highest item plus gap.
   @returns {number[]} rows + 1 ascending offsets */
function rowTops(count, columns, gap, heightOf) {
    const tops = [0];
    for (let from = 0; from < count; from += columns) {
        let height = 0;
        for (let i = from; i < Math.min(count, from + columns); i++) height = Math.max(height, heightOf(i));
        tops.push(tops[tops.length - 1] + height + gap);
    }
    return tops;
}

/* Returns the row of tops whose span holds offset y, the first or last row for offsets outside.
   @returns {number} */
function rowAt(tops, y) {
    let lo = 0, hi = tops.length - 2;
    while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (tops[mid] <= y) lo = mid; else hi = mid - 1;
    }
    return lo;
}

/* Returns the rows to render: those overlapping the viewport of a scroll container at scrollTop with
   viewportHeight, for a list starting listTop into its content with rows at the offsets tops, plus buffer rows.
   @returns {{first: number, last: number}} inclusive row range, last < first when nothing is to be rendered;
   a scrollTop past the end gives the last row */
function listWindow(scrollTop, viewportHeight, listTop, tops, buffer) {
    const rowCount = tops.length - 1;
    if (rowCount <= 0) return { first: 0, last: -1 };
    const top = scrollTop - listTop;
    const first = Math.max(0, rowAt(tops, Math.max(0, top)) - buffer);
    const last = Math.min(rowCount - 1, rowAt(tops, top + viewportHeight) + buffer);
    return { first: Math.min(first, Math.max(last, 0)), last };
}

/* Returns the element the page scrolls in.
   @returns {Element} */
function listScroller() {
    return $q("main.main");
}

/* Returns how far container starts below the top of the content of scroller.
   @returns {number} pixels */
function listTopOf(container, scroller) {
    return container.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
}

/* Returns the container of the shown view and whether it is the table.
   @returns {{container: Element | null, table: boolean}} */
function shownList() {
    const table = !!$("file-list")?.classList.contains("view-table");
    return { container: $q(table ? ".file-table tbody" : ".file-grid"), table };
}

/* Builds an empty table row that stands in for the rows outside the window.
   @returns {HTMLTableRowElement} */
function spacerRow() {
    const row = Object.assign(document.createElement("tr"), { className: "list-spacer" });
    row.setAttribute("aria-hidden", "true");
    row.appendChild(Object.assign(document.createElement("td"), { colSpan: 5 }));
    return row;
}

/* Removes the node of listing.visible[index] from the page. */
function dropNode(index) {
    const node = listView.nodes.get(index);
    previewObserver?.unobserve(node);
    node.remove();
    listView.nodes.delete(index);
}

/* Measures the shown view on a probe node of the first entry and computes the row offsets: every row as high
   as the probe, in the natural card format as high as its highest card by the aspect ratios of the entries. */
function measureList(container, table) {
    const probe = (table ? rowNode : cardNode)({ ...listing.visible[0], ar: 0 }, 0);
    container.prepend(probe);
    const gap = table ? 0 : parseFloat(getComputedStyle(container).rowGap) || 0;
    const height = probe.getBoundingClientRect().height;
    const box = table ? null : probe.querySelector(".card-preview");
    const boxWidth = box?.clientWidth ?? 0, boxHeight = box?.getBoundingClientRect().height ?? 0;
    listView.columns = table ? 1 : Math.max(1, getComputedStyle(container).gridTemplateColumns.split(" ").length);
    listView.viewHeight = window.innerHeight;
    previewObserver?.unobserve(probe);
    probe.remove();
    const natural = !table && !!$("file-list")?.classList.contains("cards-natural");
    const maxBox = CARD_RATIO_MAX_VH * window.innerHeight;
    const heightOf = !natural ? () => height : i => {
        const e = listing.visible[i];
        if (e.dir || !(e.ar > 0)) return height;
        return height - boxHeight + Math.min(maxBox, Math.max(CARD_RATIO_MIN, boxWidth / e.ar));
    };
    listView.tops = rowTops(listing.visible.length, listView.columns, gap, heightOf);
}

/* Lays the list out anew at the next frame, keeping the first visible entry in place. */
function relayoutList() {
    listView.stale = true;
    scheduleWindow();
}

/* Renders the entries of listing.visible that fall into the window of the shown view and removes the rest;
   when listView.stale it re-measures and keeps the first visible entry where it was. */
function renderWindow() {
    const { container, table } = shownList();
    if (!container || !listing.visible.length) return;
    const scroller = listScroller();
    let anchor = null;
    const y = scroller.scrollTop - listTopOf(container, scroller);
    if (listView.stale && listView.tops && y > 0) {
        const row = rowAt(listView.tops, y);
        const span = listView.tops[row + 1] - listView.tops[row];
        anchor = { index: row * listView.columns, within: span > 0 ? Math.min(1, (y - listView.tops[row]) / span) : 0 };
    }
    if (listView.stale || !listView.tops) measureList(container, table);
    listView.stale = false;
    const { tops, columns, nodes } = listView;
    const rows = tops.length - 1;
    const listTop = listTopOf(container, scroller);
    let scrollTop = scroller.scrollTop;
    if (anchor !== null) {
        const row = Math.min(rows - 1, Math.floor(anchor.index / columns));
        scrollTop = listTop + tops[row] + anchor.within * (tops[row + 1] - tops[row]);
    }
    const { first, last } = listWindow(scrollTop, scroller.clientHeight, listTop, tops, LIST_BUFFER_ROWS);
    const from = first * columns, to = Math.min(listing.visible.length, (last + 1) * columns);
    for (const index of [...nodes.keys()]) if (index < from || index >= to) dropNode(index);
    const keptFrom = nodes.size ? Math.min(...nodes.keys()) : Infinity;
    const make = table ? rowNode : cardNode;
    const head = [], tail = [];
    for (let i = from; i < to; i++) {
        if (nodes.has(i)) continue;
        const node = make(listing.visible[i], i);
        nodes.set(i, node);
        (i < keptFrom ? head : tail).push(node);
    }
    const above = tops[first], below = Math.max(0, tops[rows] - tops[last + 1]);
    if (table) {
        listView.top ??= spacerRow();
        listView.bottom ??= spacerRow();
        if (!listView.top.isConnected) container.prepend(listView.top);
        listView.top.firstChild.style.height = above + "px";
        listView.top.after(...head);
        if (listView.bottom.isConnected) listView.bottom.before(...tail); else container.append(...tail);
        if (below) { listView.bottom.firstChild.style.height = below + "px"; container.append(listView.bottom); }
        else listView.bottom.remove();
    } else {
        container.prepend(...head);
        container.append(...tail);
        container.style.paddingTop = above + "px";
        container.style.paddingBottom = below + "px";
    }
    if (anchor !== null) scroller.scrollTop = scrollTop;
}

/* Renders the window at the next animation frame, at most once per frame. */
function scheduleWindow() {
    if (listView.scheduled) return;
    listView.scheduled = true;
    requestAnimationFrame(() => { listView.scheduled = false; renderWindow(); });
}

/* Renders listing.visible afresh into the shown view, empties the other and scrolls back to the list
   when the list top lies above the viewport. */
function renderList() {
    const grid = $q(".file-grid"), body = $q(".file-table tbody");
    if (!grid || !body) return;
    for (const index of [...listView.nodes.keys()]) dropNode(index);
    grid.replaceChildren();
    body.replaceChildren();
    grid.style.paddingTop = grid.style.paddingBottom = "";
    listView.tops = null;
    listView.stale = false;
    const { container } = shownList();
    const scroller = listScroller();
    const listTop = listTopOf(container, scroller);
    if (scroller.scrollTop > listTop) scroller.scrollTop = listTop;
    renderWindow();
    const empty = $("filter-empty");
    if (empty) empty.hidden = listing.visible.length > 0;
}

/* Re-renders the window on scrolling and when the scroller resizes; re-measures it, keeping the first
   visible entry in place, when the list changes width. */
function initListWindow() {
    const list = $("file-list");
    if (!list) return;
    const scroller = listScroller();
    scroller.addEventListener("scroll", scheduleWindow, { passive: true });
    const observer = new ResizeObserver(entries => {
        for (const entry of entries) {
            const width = Math.round(entry.contentRect.width);
            if (entry.target === list && width !== listView.width) {
                listView.width = width;
                listView.stale = true;
            }
        }
        if (window.innerHeight !== listView.viewHeight && list.classList.contains("cards-natural")) listView.stale = true;
        scheduleWindow();
    });
    observer.observe(list);
    observer.observe(scroller);
}

document.addEventListener("DOMContentLoaded", initListWindow);
