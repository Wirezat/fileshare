// ── Lightbox ──────────────────────────────────────────
const lightbox = { entry: null, rendered: true, token: null, returnFocus: null, editing: false, modified: false, text: null, ask: null };
const TEXT_EDIT_MAX = 2 * 1024 * 1024;
const CARD_PREVIEW_MAX = 1200;
const CONVERTED_MAX = 2400;
const SWIPE_MIN = 50;
const TEXT_VIEWS = new Set(["text", "markdown", "html"]);
const SOURCE_ICON = "M8 6l-6 6 6 6M16 6l6 6-6 6";
const RENDERED_ICON = "M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 100 6 3 3 0 000-6z";

/* Returns the shown file entries, without folders, in listing order.
   @returns {object[]} */
function lightboxEntries() {
    return listing.visible.filter(e => !e.dir);
}

/* Shows text below the lightbox content; an empty text hides the note. */
function setNote(text) {
    const note = $("lightbox-note");
    note.textContent = text;
    note.hidden = !text;
}

/* Replaces the lightbox content with the share text key. */
function showMessage(key) {
    const msg = Object.assign(document.createElement("div"), { className: "lightbox-message", textContent: t(key) });
    $("lightbox-body").replaceChildren(msg);
}

/* Shows the info view of entry (icon, name, size, download) with the share text textKey. */
function showInfo(d, textKey) {
    const node = $("lightbox-info").content.firstElementChild.cloneNode(true);
    node.querySelector(".lightbox-info-icon").textContent = d.icon;
    node.querySelector(".lightbox-info-name").textContent = d.name;
    node.querySelector(".lightbox-info-size").textContent = formatBytes(d.size);
    node.querySelector(".lightbox-info-text").textContent = t(textKey);
    node.querySelector(".lightbox-info-download").href = d.href + "?dl=1";
    $("lightbox-body").replaceChildren(node);
}

/* Creates a highlighter that runs highlight.js in its own worker, started on first use.
   highlight(text, name) resolves to the highlighted HTML by the language of the file name, or null when the language is
   unknown or the work was cancelled or failed; cancel() stops the worker and ends every waiting call with null.
   @returns {{highlight: function(string, string): Promise<string|null>, cancel: function(): void}} */
function createHighlighter() {
    let worker = null;
    let nextId = 0;
    const pending = new Map();
    const cancel = () => {
        worker?.terminate();
        worker = null;
        pending.forEach(resolve => resolve(null));
        pending.clear();
    };
    const highlight = (text, name) => {
        if (!worker) {
            worker = new Worker("/static/share-highlight-worker.js");
            worker.onmessage = ({ data }) => {
                pending.get(data.id)?.(data.html);
                pending.delete(data.id);
            };
            worker.onerror = cancel;
        }
        const id = nextId++;
        return new Promise(resolve => {
            pending.set(id, resolve);
            worker.postMessage({ id, name, text });
        });
    };
    return { highlight, cancel };
}

const lightboxHighlighter = createHighlighter();
const cardHighlighter = createHighlighter();

/* Replaces the text of pre with its highlighted HTML unless the lightbox moved on to another token meanwhile. */
async function highlightPre(pre, name, token) {
    const html = await lightboxHighlighter.highlight(pre.textContent, name);
    if (html !== null && lightbox.token === token) pre.innerHTML = html;
}

/* Replaces the text of a card preview pre with its highlighted HTML unless the card left the page meanwhile. */
async function highlightCardLines(pre, name) {
    const html = await cardHighlighter.highlight(pre.textContent, name);
    if (html !== null && pre.isConnected) pre.innerHTML = html;
}

/* Asks the server which view a file shows judging by its first bytes.
   @returns {Promise<string>} the view name, "probe" when the file stays unknown or the request failed */
async function probeView(entry) {
    try {
        const res = await fetch(entry.href + "?view=probe");
        return res.ok ? (await res.json()).view : "probe";
    } catch {
        return "probe";
    }
}

/* Shows an entry without a known extension in the view its content points to; unknown content is shown as text. */
async function renderProbed(d) {
    const token = lightbox.token = {};
    showMessage("share.lightbox.loading");
    const view = await probeView(d);
    if (lightbox.token !== token) return;
    if (view === "probe") showFragment(d, "text");
    else renderEntry({ ...d, view });
}

/* Loads the ?view=<view> fragment of entry into the lightbox; an unknown binary file shows the info view. */
async function showFragment(entry, view) {
    const token = lightbox.token = {};
    showMessage("share.lightbox.loading");
    try {
        const res = await fetch(entry.href + "?view=" + view);
        if (lightbox.token !== token) return;
        if (res.status === 415) { showInfo(entry, "share.lightbox.no_preview"); return; }
        if (!res.ok) throw new Error(String(res.status));
        const html = await res.text();
        if (lightbox.token !== token) return;
        const panel = Object.assign(document.createElement("div"), { className: "lightbox-text" });
        panel.innerHTML = html;
        $("lightbox-body").replaceChildren(panel);
        setNote(res.headers.get("X-Fileshare-Truncated") === "1" ? t("share.lightbox.truncated") : "");
        if (panel.firstElementChild?.tagName === "PRE") highlightPre(panel.firstElementChild, entry.name, token);
    } catch {
        if (lightbox.token === token) showMessage("share.lightbox.failed");
    }
}

/* Builds the iframe for src; sandboxed adds an empty sandbox attribute.
   @returns {HTMLIFrameElement} */
function lightboxFrame(src, title, sandboxed) {
    const frame = Object.assign(document.createElement("iframe"), { className: "lightbox-frame", title });
    if (sandboxed) frame.setAttribute("sandbox", "");
    frame.src = src;
    return frame;
}

/* Wraps media with the elements extra and a lightbox media menu; when media cannot be played, a note replaces the menu.
   @returns {HTMLDivElement} */
function mediaPlayer(media, extra) {
    const wrap = Object.assign(document.createElement("div"), { className: "lightbox-player" });
    media.className = "lightbox-media";
    media.preload = "metadata";
    media.playsInline = true;
    const menu = createMediaMenu(media, "lightbox", wrap);
    media.addEventListener("error", () => {
        menu.remove();
        if (wrap.isConnected) setNote(t("share.lightbox.unplayable"));
    });
    wrap.append(...extra, media, menu);
    return wrap;
}

/* Fills the lightbox body for entry according to its view. */
function renderEntry(d) {
    const body = $("lightbox-body");
    const source = $("lightbox-source");
    lightbox.token = {};
    lightboxHighlighter.cancel();
    setNote("");
    body.querySelectorAll("audio, video").forEach(m => m.pause());
    body.replaceChildren();
    source.hidden = d.size === 0 || (d.view !== "markdown" && d.view !== "html");
    const label = t(lightbox.rendered ? "share.lightbox.source" : "share.lightbox.rendered");
    source.replaceChildren(svgIcon(lightbox.rendered ? SOURCE_ICON : RENDERED_ICON));
    source.title = label;
    source.setAttribute("aria-label", label);

    if (d.size === 0) { showInfo(d, "share.lightbox.empty"); return; }
    if (!lightbox.rendered && !source.hidden) { showFragment(d, "source"); return; }
    switch (d.view) {
        case "image":
        case "converted": {
            const img = Object.assign(document.createElement("img"), { className: "lightbox-image", alt: d.name });
            img.addEventListener("error", () => showMessage("share.lightbox.failed"));
            img.src = d.view === "converted" ? `${d.href}?preview=1&size=${CONVERTED_MAX}` : d.href;
            body.appendChild(img);
            break;
        }
        case "video": {
            const video = document.createElement("video");
            setVideoPoster(video, `${d.href}?preview=1&size=${CARD_PREVIEW_MAX}`);
            video.src = d.href;
            body.appendChild(mediaPlayer(video, []));
            break;
        }
        case "audio": {
            const cover = Object.assign(document.createElement("img"), { className: "lightbox-cover", alt: "" });
            cover.addEventListener("error", () => {
                cover.replaceWith(Object.assign(document.createElement("span"), {
                    className: "lightbox-cover lightbox-cover-icon",
                    textContent: d.icon,
                }));
            });
            cover.src = `${d.href}?preview=1&size=${CARD_PREVIEW_MAX}`;
            const audio = document.createElement("audio");
            audio.src = d.href;
            body.appendChild(mediaPlayer(audio, [cover]));
            break;
        }
        case "pdf":
            body.appendChild(lightboxFrame(d.href + "?view=frame", d.name, false));
            break;
        case "office":
            body.appendChild(lightboxFrame(d.href + "?view=office", d.name, false));
            break;
        case "html":
            body.appendChild(lightboxFrame(d.href + "?view=frame", d.name, true));
            break;
        case "probe":
            renderProbed(d);
            break;
        case "markdown":
        case "text":
            showFragment(d, "text");
            break;
        default:
            showInfo(d, "share.lightbox.no_preview");
    }
}

/* Opens the lightbox on a listing entry and pauses every other medium. */
function openLightbox(d) {
    const box = $("lightbox");
    pauseOtherMedia(null);
    if (box.hidden) lightbox.returnFocus = document.activeElement;
    lightbox.entry = d;
    lightbox.rendered = true;

    $("lightbox-name").textContent = d.name;
    $("lightbox-size").textContent = formatBytes(d.size);
    $("lightbox-download").href = d.href + "?dl=1";
    $("lightbox-edit").hidden = !editable(d);
    const several = lightboxEntries().length > 1;
    $("lightbox-prev").hidden = $("lightbox-next").hidden = !several;

    renderEntry(d);
    if (box.hidden) {
        box.hidden = false;
        document.body.style.overflow = "hidden";
        $("lightbox-close").focus();
    }
}

/* Reports whether entry d can be edited in this share: office documents, and text files up to 2 MiB.
   @returns {boolean} */
function editable(d) {
    const grid = document.querySelector(".file-grid")?.dataset ?? {};
    if (d.view === "office") return grid.office === "edit";
    return grid.edit === "text" && TEXT_VIEWS.has(d.view) && d.size <= TEXT_EDIT_MAX;
}

/* Shows the message of key as a toast of tone. */
function toast(key, tone = "success") {
    import("/static/ui/js/components/toast.js").then(m => m.showToast({ tone, messageKey: t(key) })).catch(() => {});
}

/* Shows the message of messageKey with one button per choice [key, labelKey] in the bar above the lightbox body.
   @returns {Promise<string | null>} the key of the chosen button, null when the question is dropped */
function askLightbox(messageKey, choices) {
    const bar = $("lightbox-ask");
    lightbox.ask?.(null);
    return new Promise(resolve => {
        const done = key => {
            if (lightbox.ask !== done) return;
            lightbox.ask = null;
            bar.hidden = true;
            resolve(key);
        };
        lightbox.ask = done;
        const buttons = choices.map(([key, labelKey], i) => {
            const b = Object.assign(document.createElement("button"), {
                type: "button", className: `btn btn-sm ${i === 0 ? "btn-primary" : "btn-ghost"}`, textContent: t(labelKey),
            });
            b.addEventListener("click", () => done(key));
            return b;
        });
        bar.replaceChildren(Object.assign(document.createElement("span"), { textContent: t(messageKey) }), ...buttons);
        bar.hidden = false;
        buttons[0].focus();
    });
}

/* Switches the lightbox to the wide editing mode, in which browsing to other files stops until editing ends. */
function enterWide() {
    lightbox.token = {};
    lightbox.editing = true;
    lightbox.modified = false;
    $("lightbox").classList.add("lightbox-wide");
    $("lightbox-edit").hidden = $("lightbox-source").hidden = true;
    $("lightbox-prev").hidden = $("lightbox-next").hidden = true;
}

/* Opens the editor that fits the shown entry. */
function startEdit() {
    if (lightbox.entry?.view === "office") editOffice();
    else editText();
}

/* Edits the shown office document in the document server. */
function editOffice() {
    const d = lightbox.entry;
    enterWide();
    const frame = lightboxFrame(d.href + "?view=office&edit=1", d.name, false);
    $("lightbox-body").replaceChildren(frame);
    frame.focus();
}

/* Loads the shown text file with its entity tag into a code editor. */
async function editText() {
    const d = lightbox.entry;
    enterWide();
    const token = lightbox.token;
    showMessage("share.lightbox.loading");
    try {
        const [res, cm] = await Promise.all([fetch(d.href, { cache: "no-store" }), import("/static/codemirror/editor.js")]);
        if (!res.ok) throw new Error(res.statusText);
        const text = await res.text();
        if (lightbox.token !== token) return;
        const host = Object.assign(document.createElement("div"), { className: "lightbox-editor" });
        $("lightbox-body").replaceChildren(host);
        const session = { entry: d, etag: res.headers.get("ETag"), dirty: false, saving: false };
        session.editor = cm.createEditor(host, {
            text,
            filename: d.name,
            dark: document.documentElement.classList.contains("dark"),
            onSave: () => saveText(),
            onChange: dirty => {
                session.dirty = dirty;
                $("lightbox-save").disabled = !dirty;
            },
        });
        lightbox.text = session;
        $("lightbox-save").hidden = $("lightbox-discard").hidden = false;
        $("lightbox-save").disabled = true;
        session.editor.focus();
    } catch {
        if (lightbox.token === token) showMessage("share.edit.load_failed");
    }
}

/* Replaces the text of the editor with the current version of the file. */
async function reloadText() {
    const s = lightbox.text;
    const res = await fetch(s.entry.href, { cache: "no-store" });
    if (!res.ok) throw new Error(res.statusText);
    const text = await res.text();
    if (lightbox.text !== s) return;
    s.etag = res.headers.get("ETag");
    s.editor.reset(text);
}

/* Saves the edited text; force overwrites a version changed in the meantime. A conflict asks whether to overwrite
   the other version or to load it. */
async function saveText(force = false) {
    const s = lightbox.text;
    if (!s || s.saving) return;
    s.saving = true;
    let conflict = false;
    try {
        const etag = force ? (await fetch(s.entry.href, { method: "HEAD", cache: "no-store" })).headers.get("ETag") : s.etag;
        const sent = s.editor.text();
        const res = await fetch(s.entry.href, {
            method: "PUT",
            headers: { "If-Match": etag ?? "", "Content-Type": "text/plain; charset=utf-8" },
            body: sent,
        });
        if (res.status === 204) {
            s.etag = res.headers.get("ETag");
            s.entry.size = new Blob([sent]).size;
            $("lightbox-size").textContent = formatBytes(s.entry.size);
            if (s.editor.text() === sent) s.editor.markClean();
            toast("share.edit.saved");
        } else if (res.status === 412) {
            conflict = true;
        } else {
            toast(res.status === 413 ? "share.edit.too_large" : "share.edit.save_failed", "danger");
        }
    } catch {
        toast("share.edit.save_failed", "danger");
    } finally {
        s.saving = false;
    }
    if (!conflict) return;
    const choice = await askLightbox("share.edit.conflict",
        [["overwrite", "share.edit.overwrite"], ["reload", "share.edit.reload"], ["cancel", "share.edit.cancel"]]);
    if (lightbox.text !== s) return;
    if (choice === "overwrite") saveText(true);
    else if (choice === "reload") reloadText().catch(() => toast("share.edit.load_failed", "danger"));
}

/* Resolves true when unsaved text changes may be dropped: there are none, or the user agrees.
   @returns {Promise<boolean>} */
async function mayDropText() {
    if (!lightbox.text?.dirty) return true;
    return await askLightbox("share.edit.unsaved", [["discard", "share.edit.discard"], ["cancel", "share.edit.cancel"]]) === "discard";
}

/* Leaves the editing mode; after office changes it tells that the document server saves them now. */
function endEdit() {
    if (!lightbox.editing) return;
    if (lightbox.modified) toast("share.office.saving");
    lightbox.text?.editor.destroy();
    lightbox.text = null;
    lightbox.ask?.(null);
    $("lightbox-save").hidden = $("lightbox-discard").hidden = true;
    lightbox.editing = lightbox.modified = false;
    $("lightbox").classList.remove("lightbox-wide");
}

/* Drops the text changes after asking and shows the file read-only again. */
async function discardEdit() {
    if (!await mayDropText()) return;
    const d = lightbox.entry;
    endEdit();
    openLightbox(d);
}

/* Closes the lightbox, stops its media and returns focus to where it was; unsaved text changes are asked about first. */
async function closeLightbox() {
    const box = $("lightbox");
    if (box.hidden || !await mayDropText()) return;
    endEdit();
    lightbox.token = {};
    lightboxHighlighter.cancel();
    $("lightbox-body").querySelectorAll("audio, video").forEach(m => m.pause());
    $("lightbox-body").replaceChildren();
    box.hidden = true;
    document.body.style.overflow = "";
    lightbox.entry = null;
    lightbox.returnFocus?.focus?.();
}

/* Moves to the previous (-1) or next (1) visible file, wrapping around. */
function stepLightbox(dir) {
    if (lightbox.editing) return;
    const list = lightboxEntries();
    if (!list.length) return;
    const i = list.indexOf(lightbox.entry);
    openLightbox(list[((i < 0 ? 0 : i + dir) + list.length) % list.length]);
}

/* Wires close, navigation, source toggle, keys and swipe of the lightbox. */
function initLightbox() {
    const box = $("lightbox");
    if (!box) return;
    $("lightbox-close").addEventListener("click", closeLightbox);
    $("lightbox-edit").addEventListener("click", startEdit);
    $("lightbox-save").addEventListener("click", () => saveText());
    $("lightbox-discard").addEventListener("click", discardEdit);
    window.addEventListener("beforeunload", e => { if (lightbox.text?.dirty) e.preventDefault(); });
    window.addEventListener("message", e => {
        if (e.origin === location.origin && e.data?.type === "fileshare-office-state" && lightbox.editing && e.data.modified) {
            lightbox.modified = true;
        }
    });
    $("lightbox-prev").addEventListener("click", () => stepLightbox(-1));
    $("lightbox-next").addEventListener("click", () => stepLightbox(1));
    box.addEventListener("click", e => { if (e.target === box) closeLightbox(); });
    $("lightbox-source").addEventListener("click", () => {
        lightbox.rendered = !lightbox.rendered;
        renderEntry(lightbox.entry);
    });
    document.addEventListener("keydown", e => {
        if (box.hidden) return;
        if (e.key === "Escape") {
            if (!e.target.closest?.(".cm-editor, .lightbox-ask")) closeLightbox();
        }
        else if (e.target.closest?.(".media-seek, input")) return;
        else if (e.key === "ArrowRight") stepLightbox(1);
        else if (e.key === "ArrowLeft") stepLightbox(-1);
    });
    let start = null;
    const stage = $("lightbox-body");
    stage.addEventListener("touchstart", e => {
        start = e.touches.length === 1 ? { x: e.touches[0].clientX, y: e.touches[0].clientY } : null;
    }, { passive: true });
    stage.addEventListener("touchend", e => {
        if (!start || e.target.closest(".media-menu")) return;
        const dx = e.changedTouches[0].clientX - start.x, dy = e.changedTouches[0].clientY - start.y;
        start = null;
        if (Math.abs(dx) > SWIPE_MIN && Math.abs(dx) > Math.abs(dy)) stepLightbox(dx < 0 ? 1 : -1);
    });
}

// ── Entry clicks ──────────────────────────────────────
/* Opens the lightbox for a plain click on a file card or row, leaving folders, modifiers and controls alone. */
function initEntryClicks() {
    document.addEventListener("click", e => {
        if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
        if (e.target.closest(".select-box, .row-select, .card-download, .row-download, .media-menu")) return;
        const entry = entryAt(e.target);
        if (!entry || entry.dir) return;
        e.preventDefault();
        openLightbox(entry);
    });
}

// ── Card previews ─────────────────────────────────────
/* Returns the pixel size to request for a preview filling box: its long edge in device pixels, at most 1200.
   @returns {number} */
function cardPixels(box) {
    const natural = box.closest(".cards-natural") ? 2 : 1;
    return Math.min(CARD_PREVIEW_MAX, Math.ceil(Math.max(box.clientWidth * natural, box.clientHeight) * (window.devicePixelRatio || 1)));
}

/* Shows the embedded cover at url as poster of video once it has loaded; without a cover the first frame stays.
   @returns {HTMLImageElement} the image that loads the cover */
function setVideoPoster(video, url) {
    const cover = new Image();
    cover.addEventListener("load", () => { video.poster = url; });
    cover.src = url;
    return cover;
}

/* Gives the preview box of card the aspect ratio w/h of its loaded medium when the listing did not know it
   or knew another one, and lays the list out anew. */
function setCardRatio(card, box, w, h) {
    const entry = entryAt(card);
    if (!entry || !(w > 0 && h > 0) || Math.abs((entry.ar || 0) - w / h) < 0.01) return;
    entry.ar = w / h;
    applyCardRatio(box, entry.ar);
    relayoutList();
}

/* Loads the preview of entry d into its card according to its view; on 204 or error the icon stays. */
function previewCard(card, d) {
    const box = card.querySelector(".card-preview");
    if (!box || d.size === 0) return;
    if (d.view === "probe") {
        probeView(d).then(view => previewCard(card, { ...d, view: view === "probe" ? "text" : view }));
        return;
    }
    const shown = () => box.classList.add("has-preview");
    if (d.view === "image" || d.view === "converted" || d.view === "audio") {
        const img = Object.assign(document.createElement("img"), { className: "card-thumb", alt: "", decoding: "async" });
        img.addEventListener("load", () => { setCardRatio(card, box, img.naturalWidth, img.naturalHeight); shown(); });
        img.addEventListener("error", () => img.remove());
        img.src = /\.svg$/i.test(d.name) ? d.href : `${d.href}?preview=1&size=${cardPixels(box)}`;
        box.appendChild(img);
    }
    if (d.view === "audio") {
        const audio = Object.assign(document.createElement("audio"), { preload: "metadata", src: d.href });
        const menu = createMediaMenu(audio, "card");
        audio.addEventListener("error", () => menu.remove());
        box.append(audio, menu);
    } else if (d.view === "video") {
        const video = Object.assign(document.createElement("video"), { className: "card-thumb", preload: "metadata", playsInline: true, src: d.href });
        setVideoPoster(video, `${d.href}?preview=1&size=${cardPixels(box)}`).addEventListener("load", shown);
        video.addEventListener("loadedmetadata", () => setCardRatio(card, box, video.videoWidth, video.videoHeight));
        video.addEventListener("loadeddata", shown);
        const menu = createMediaMenu(video, "card");
        video.addEventListener("error", () => menu.remove());
        box.append(video, menu);
    } else if (TEXT_VIEWS.has(d.view)) {
        fetch(d.href + "?preview=1")
            .then(res => res.status === 200 ? res.text() : null)
            .then(text => {
                if (text == null) return;
                const pre = Object.assign(document.createElement("pre"), { className: "card-lines", textContent: text });
                box.appendChild(pre);
                shown();
                highlightCardLines(pre, d.name);
            })
            .catch(() => {});
    }
}

const PREVIEW_DELAY_MS = 150;
const previewTimers = new WeakMap();
let previewObserver = null;

/* Loads the preview of a file card once it has stayed near the viewport for PREVIEW_DELAY_MS. */
function observePreview(card) {
    previewObserver ??= new IntersectionObserver(entries => entries.forEach(en => {
        clearTimeout(previewTimers.get(en.target));
        if (!en.isIntersecting) return;
        previewTimers.set(en.target, setTimeout(() => {
            if (!en.target.isConnected) return;
            previewObserver.unobserve(en.target);
            const entry = entryAt(en.target);
            if (entry) previewCard(en.target, entry);
        }, PREVIEW_DELAY_MS));
    }), { rootMargin: "200px" });
    previewObserver.observe(card);
}

document.addEventListener("DOMContentLoaded", () => {
    initLightbox();
    initEntryClicks();
});
