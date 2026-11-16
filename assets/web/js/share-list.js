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

const LIST_BUFFER_ROWS = 3;
const listView = { nodes: new Map(), rowHeight: 0, columns: 1, width: 0, stale: false, scheduled: false, top: null, bottom: null };

/* Returns the rows to render: those overlapping the viewport of a scroll container at scrollTop with
   viewportHeight, for a list starting listTop into its content with rowCount rows of rowHeight, plus buffer rows.
   @returns {{first: number, last: number}} inclusive row range, last < first when nothing is to be rendered;
   a scrollTop past the end gives the last row */
function listWindow(scrollTop, viewportHeight, listTop, rowHeight, rowCount, buffer) {
    if (rowCount === 0) return { first: 0, last: -1 };
    const top = scrollTop - listTop;
    const first = Math.max(0, Math.floor(Math.max(0, top) / rowHeight) - buffer);
    const last = Math.min(rowCount - 1, Math.ceil((top + viewportHeight) / rowHeight) - 1 + buffer);
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

/* Measures row height and column count of the shown view on a probe node of the first entry. */
function measureList(container, table) {
    const probe = (table ? rowNode : cardNode)(listing.visible[0], 0);
    container.prepend(probe);
    const gap = table ? 0 : parseFloat(getComputedStyle(container).rowGap) || 0;
    listView.rowHeight = probe.getBoundingClientRect().height + gap;
    listView.columns = table ? 1 : Math.max(1, getComputedStyle(container).gridTemplateColumns.split(" ").length);
    previewObserver?.unobserve(probe);
    probe.remove();
}

/* Renders the entries of listing.visible that fall into the window of the shown view and removes the rest;
   when listView.stale it re-measures and keeps the first visible entry where it was. */
function renderWindow() {
    const { container, table } = shownList();
    if (!container || !listing.visible.length) return;
    const scroller = listScroller();
    let anchor = null;
    if (listView.stale && listView.rowHeight) {
        const offset = Math.max(0, scroller.scrollTop - listTopOf(container, scroller)) / listView.rowHeight;
        anchor = { index: Math.floor(offset) * listView.columns, within: offset % 1 };
    }
    if (listView.stale || !listView.rowHeight) measureList(container, table);
    listView.stale = false;
    const { rowHeight, columns, nodes } = listView;
    const rows = Math.ceil(listing.visible.length / columns);
    const listTop = listTopOf(container, scroller);
    const scrollTop = anchor === null ? scroller.scrollTop
        : listTop + (Math.floor(anchor.index / columns) + anchor.within) * rowHeight;
    const { first, last } = listWindow(scrollTop, scroller.clientHeight, listTop, rowHeight, rows, LIST_BUFFER_ROWS);
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
    const above = first * rowHeight, below = Math.max(0, rows - 1 - last) * rowHeight;
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
    listView.rowHeight = 0;
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
            if (entry.target !== list || width === listView.width) continue;
            listView.width = width;
            listView.stale = true;
        }
        scheduleWindow();
    });
    observer.observe(list);
    observer.observe(scroller);
}

document.addEventListener("DOMContentLoaded", initListWindow);
