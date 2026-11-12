// ── Lightbox ──────────────────────────────────────────
const lightbox = { entry: null, rendered: true, token: null, returnFocus: null };
const CARD_PREVIEW_MAX = 1200;
const SWIPE_MIN = 50;
const TEXT_VIEWS = new Set(["text", "markdown", "html", "probe"]);
const SOURCE_ICON = "M8 6l-6 6 6 6M16 6l6 6-6 6";
const RENDERED_ICON = "M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 100 6 3 3 0 000-6z";

/* Reads the server's description of a listing entry.
   @returns {{name: string, href: string, size: number, view: string}} */
function entryData(entry) {
    return {
        name: entry.dataset.name ?? "",
        href: entry.dataset.href ?? "",
        size: Number(entry.dataset.size ?? 0),
        view: entry.dataset.view ?? "info",
    };
}

/* Returns the visible file entries of the view that is shown, in listing order.
   @returns {Element[]} */
function lightboxEntries() {
    const table = $("file-list")?.classList.contains("view-table");
    return [...document.querySelectorAll(table ? ".file-row" : ".file-card")]
        .filter(el => !el.hidden && !el.hasAttribute("data-dir"));
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
function showInfo(entry, textKey) {
    const d = entryData(entry);
    const node = $("lightbox-info").content.firstElementChild.cloneNode(true);
    node.querySelector(".lightbox-info-icon").textContent = entry.querySelector(".card-icon, .row-icon")?.textContent ?? "";
    node.querySelector(".lightbox-info-name").textContent = d.name;
    node.querySelector(".lightbox-info-size").textContent = formatBytes(d.size);
    node.querySelector(".lightbox-info-text").textContent = t(textKey);
    node.querySelector(".lightbox-info-download").href = d.href + "?dl=1";
    $("lightbox-body").replaceChildren(node);
}

/* Loads the ?view=<view> fragment of entry into the lightbox; an unknown binary file shows the info view. */
async function showFragment(entry, view) {
    const token = lightbox.token = {};
    showMessage("share.lightbox.loading");
    try {
        const res = await fetch(entryData(entry).href + "?view=" + view);
        if (lightbox.token !== token) return;
        if (res.status === 415) { showInfo(entry, "share.lightbox.no_preview"); return; }
        if (!res.ok) throw new Error(String(res.status));
        const html = await res.text();
        if (lightbox.token !== token) return;
        const panel = Object.assign(document.createElement("div"), { className: "lightbox-text" });
        panel.innerHTML = html;
        $("lightbox-body").replaceChildren(panel);
        setNote(res.headers.get("X-Fileshare-Truncated") === "1" ? t("share.lightbox.truncated") : "");
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

/* Wraps media with the elements extra and a lightbox media menu.
   @returns {HTMLDivElement} */
function mediaPlayer(media, extra) {
    const wrap = Object.assign(document.createElement("div"), { className: "lightbox-player" });
    media.className = "lightbox-media";
    media.preload = "metadata";
    media.playsInline = true;
    wrap.append(...extra, media, createMediaMenu(media, "lightbox", wrap));
    return wrap;
}

/* Fills the lightbox body for entry according to its view. */
function renderEntry(entry) {
    const d = entryData(entry);
    const body = $("lightbox-body");
    const source = $("lightbox-source");
    lightbox.token = {};
    setNote("");
    body.querySelectorAll("audio, video").forEach(m => m.pause());
    body.replaceChildren();
    source.hidden = d.size === 0 || (d.view !== "markdown" && d.view !== "html");
    const label = t(lightbox.rendered ? "share.lightbox.source" : "share.lightbox.rendered");
    source.replaceChildren(svgIcon(lightbox.rendered ? SOURCE_ICON : RENDERED_ICON));
    source.title = label;
    source.setAttribute("aria-label", label);

    if (d.size === 0) { showInfo(entry, "share.lightbox.empty"); return; }
    if (!lightbox.rendered && !source.hidden) { showFragment(entry, "source"); return; }
    switch (d.view) {
        case "image": {
            const img = Object.assign(document.createElement("img"), { className: "lightbox-image", alt: d.name });
            img.addEventListener("error", () => showMessage("share.lightbox.failed"));
            img.src = d.href;
            body.appendChild(img);
            break;
        }
        case "video": {
            const video = document.createElement("video");
            video.src = d.href;
            body.appendChild(mediaPlayer(video, []));
            break;
        }
        case "audio": {
            const cover = Object.assign(document.createElement("img"), { className: "lightbox-cover", alt: "" });
            cover.addEventListener("error", () => {
                cover.replaceWith(Object.assign(document.createElement("span"), {
                    className: "lightbox-cover lightbox-cover-icon",
                    textContent: entry.querySelector(".card-icon, .row-icon")?.textContent ?? "",
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
        case "markdown":
        case "text":
        case "probe":
            showFragment(entry, "text");
            break;
        default:
            showInfo(entry, "share.lightbox.no_preview");
    }
}

/* Opens the lightbox on a listing entry (card or row) and pauses every other medium. */
function openLightbox(entry) {
    const d = entryData(entry);
    const box = $("lightbox");
    pauseOtherMedia(null);
    if (box.hidden) lightbox.returnFocus = document.activeElement;
    lightbox.entry = entry;
    lightbox.rendered = true;

    $("lightbox-name").textContent = d.name;
    $("lightbox-size").textContent = formatBytes(d.size);
    $("lightbox-download").href = d.href + "?dl=1";
    const edit = $("lightbox-edit");
    edit.hidden = !(d.view === "office" && document.querySelector(".file-grid")?.dataset.office === "edit");
    if (edit.hidden) edit.removeAttribute("href");
    else edit.href = d.href;
    const several = lightboxEntries().length > 1;
    $("lightbox-prev").hidden = $("lightbox-next").hidden = !several;

    renderEntry(entry);
    if (box.hidden) {
        box.hidden = false;
        document.body.style.overflow = "hidden";
        $("lightbox-close").focus();
    }
}

/* Closes the lightbox, stops its media and returns focus to where it was. */
function closeLightbox() {
    const box = $("lightbox");
    if (box.hidden) return;
    lightbox.token = {};
    $("lightbox-body").querySelectorAll("audio, video").forEach(m => m.pause());
    $("lightbox-body").replaceChildren();
    box.hidden = true;
    document.body.style.overflow = "";
    lightbox.entry = null;
    lightbox.returnFocus?.focus?.();
}

/* Moves to the previous (-1) or next (1) visible file, wrapping around. */
function stepLightbox(dir) {
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
    $("lightbox-prev").addEventListener("click", () => stepLightbox(-1));
    $("lightbox-next").addEventListener("click", () => stepLightbox(1));
    box.addEventListener("click", e => { if (e.target === box) closeLightbox(); });
    $("lightbox-source").addEventListener("click", () => {
        lightbox.rendered = !lightbox.rendered;
        renderEntry(lightbox.entry);
    });
    document.addEventListener("keydown", e => {
        if (box.hidden) return;
        if (e.key === "Escape") closeLightbox();
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
        const entry = e.target.closest(".file-card, .file-row");
        if (!entry || entry.hasAttribute("data-dir")) return;
        e.preventDefault();
        openLightbox(entry);
    });
}

// ── Card previews ─────────────────────────────────────
/* Returns the pixel size to request for a preview filling box: its long edge in device pixels, at most 1200.
   @returns {number} */
function cardPixels(box) {
    return Math.min(CARD_PREVIEW_MAX, Math.ceil(Math.max(box.clientWidth, box.clientHeight) * (window.devicePixelRatio || 1)));
}

/* Loads the preview of a card according to its view; on 204 or error the icon stays. */
function previewCard(card) {
    const d = entryData(card);
    const box = card.querySelector(".card-preview");
    if (!box || d.size === 0) return;
    const shown = () => box.classList.add("has-preview");
    if (d.view === "image" || d.view === "audio") {
        const img = Object.assign(document.createElement("img"), { className: "card-thumb", alt: "", decoding: "async" });
        img.addEventListener("load", shown);
        img.addEventListener("error", () => img.remove());
        img.src = `${d.href}?preview=1&size=${cardPixels(box)}`;
        box.appendChild(img);
    }
    if (d.view === "audio") {
        const audio = Object.assign(document.createElement("audio"), { preload: "metadata", src: d.href });
        box.append(audio, createMediaMenu(audio, "card"));
    } else if (d.view === "video") {
        const video = Object.assign(document.createElement("video"), { className: "card-thumb", preload: "metadata", playsInline: true, src: d.href });
        video.addEventListener("loadeddata", shown);
        box.append(video, createMediaMenu(video, "card"));
    } else if (TEXT_VIEWS.has(d.view)) {
        fetch(d.href + "?preview=1")
            .then(res => res.status === 200 ? res.text() : null)
            .then(text => {
                if (text == null) return;
                box.appendChild(Object.assign(document.createElement("pre"), { className: "card-lines", textContent: text }));
                shown();
            })
            .catch(() => {});
    }
}

/* Loads card previews once each card comes near the viewport. */
function initCardPreviews() {
    const cards = [...document.querySelectorAll(".file-card[data-view]")];
    if (!("IntersectionObserver" in window)) { cards.forEach(previewCard); return; }
    const io = new IntersectionObserver(entries => entries.forEach(en => {
        if (!en.isIntersecting) return;
        io.unobserve(en.target);
        previewCard(en.target);
    }), { rootMargin: "200px" });
    cards.forEach(c => io.observe(c));
}

document.addEventListener("DOMContentLoaded", () => {
    initLightbox();
    initEntryClicks();
    initCardPreviews();
});
