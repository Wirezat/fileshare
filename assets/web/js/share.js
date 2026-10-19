// ── Shorthands & constants ────────────────────────────
const _dc = {};
const $ = id => _dc[id] || (_dc[id] = document.getElementById(id));
const $q = s => document.querySelector(s);

const MAX_UPLOAD_RATE = 0;
const CHUNK_SIZE = 5 * 1024 * 1024;
const MIN_CHUNK_SIZE = 256 * 1024;
const MAX_SENDING = 4;
const INIT_AHEAD = 4;
const MULTIPLEXED = /^h[23]/.test(performance.getEntriesByType?.("navigation")[0]?.nextHopProtocol ?? "");
const MAX_OPEN = MULTIPLEXED ? 16 : 6;
const QUEUE_LOW = 2 * MAX_SENDING;
const ETA_AFTER_MS = 2000;

// ── Dark mode ─────────────────────────────────────────
function toggleTheme() {
    const dark = document.documentElement.classList.toggle("dark");
    localStorage.setItem("theme", dark ? "dark" : "light");
    $("theme-toggle").textContent = dark ? "☀️" : "🌙";
}
$("theme-toggle").textContent = document.documentElement.classList.contains("dark") ? "☀️" : "🌙";

// ── Lightbox ──────────────────────────────────────────
let isLightboxOpen = false, currentMediaIndex = 0;
const mediaElements = [];

function openLightbox(mediaEl) {
    isLightboxOpen = true;
    const img = $("lightbox-img"), vid = $("lightbox-video");
    img.style.display = vid.style.display = $("lightbox-text").style.display = "none";
    currentMediaIndex = mediaElements.indexOf(mediaEl);
    if (mediaEl.tagName === "IMG") {
        img.src = mediaEl.src;
        img.style.display = "block";
    } else {
        vid.querySelector("source").src = mediaEl.querySelector("source").src;
        vid.load();
        vid.style.display = "block";
    }
    $("lightbox").style.display = "block";
    document.body.style.overflow = "hidden";
}

function closeLightbox() {
    isLightboxOpen = false;
    $("lightbox-video").pause();
    $("lightbox").style.display = "none";
    document.body.style.overflow = "";
}

async function openTextLightbox(href) {
    const panel = $("lightbox-text");
    $("lightbox-img").style.display = $("lightbox-video").style.display = "none";
    panel.textContent = "Loading…";
    panel.style.display = "block";
    $("lightbox").style.display = "block";
    document.body.style.overflow = "hidden";
    isLightboxOpen = true;
    try {
        const res = await fetch(href + "?view=text");
        if (!res.ok) throw new Error(await res.text());
        panel.innerHTML = await res.text();
    } catch (err) {
        panel.textContent = String(err.message || err).trim() || "Preview failed.";
    }
}

function initTextPreview() {
    document.addEventListener("click", e => {
        const link = e.target.closest("[data-text] a:not(.row-download):not(.download-button)");
        if (!link || link.hasAttribute("download") || e.ctrlKey || e.metaKey || e.button !== 0) return;
        e.preventDefault();
        openTextLightbox(link.closest("[data-text]").dataset.href);
    });
}

function navigateLightbox(dir) {
    if (!mediaElements.length) return;
    currentMediaIndex = (currentMediaIndex + dir + mediaElements.length) % mediaElements.length;
    openLightbox(mediaElements[currentMediaIndex]);
}

// ── File cards ────────────────────────────────────────
const MEDIA_KIND = {
    jpg: "image", jpeg: "image", png: "image", gif: "image", webp: "image", avif: "image",
    mp4: "video", mov: "video", webm: "video", mkv: "video", avi: "video", wmv: "video",
    mp3: "audio", wav: "audio", flac: "audio", aac: "audio", ogg: "audio",
};

const KIND_ICON = {
    zip: "📦", rar: "📦", "7z": "📦", tar: "📦", gz: "📦",
    pdf: "📄", doc: "📄", docx: "📄", odt: "📄",
    xls: "📊", xlsx: "📊", ods: "📊", csv: "📊",
    ppt: "📽️", pptx: "📽️", odp: "📽️",
    txt: "📝", md: "📝", log: "📝",
};

const OFFICE_EXT = new Set(["docx", "doc", "odt", "xlsx", "xls", "ods", "pptx", "ppt", "odp"]);
const officeOn = document.querySelector(".file-grid")?.hasAttribute("data-office") ?? false;

const opensInline = ext => ext === "pdf" || (officeOn && OFFICE_EXT.has(ext));

function downloadOverlay(href) {
    const overlay = document.createElement("div");
    overlay.className = "overlay";
    const btn = document.createElement("a");
    btn.className = "btn btn-primary btn-sm download-button";
    btn.href = href + "?dl=1";
    btn.download = "";
    btn.textContent = "Download";
    btn.addEventListener("click", e => e.stopPropagation());
    overlay.appendChild(btn);
    return overlay;
}

function hydrateCards() {
    document.querySelectorAll(".file-card").forEach(card => {
        const name = card.dataset.name ?? "";
        const href = card.dataset.href ?? "";
        let preview = card.querySelector(".card-preview");
        if (!preview) return;

        const sizeEl = card.querySelector(".card-size");
        if (sizeEl && !card.hasAttribute("data-dir")) {
            sizeEl.textContent = formatBytes(Number(card.dataset.size ?? 0));
        }

        if (card.hasAttribute("data-dir")) return;

        const ext = name.split(".").pop().toLowerCase();
        const kind = MEDIA_KIND[ext];

        if (!kind) {
            const inline = opensInline(ext) || card.hasAttribute("data-text");
            card.classList.toggle("file-card-inline", inline);
            card.querySelector(".card-icon").textContent = inline ? "📖" : (KIND_ICON[ext] ?? "📎");
            card.querySelector(".card-ext").textContent = fileTypeLabel(name);
            card.appendChild(downloadOverlay(href));
            return;
        }

        // media-container is the lightbox hook; audio has nothing to enlarge.
        card.classList.add("file-card-" + kind);
        if (kind !== "audio") card.classList.add("media-container");

        // Swap the server-rendered preview anchor for a div; media cards open
        // the lightbox instead of navigating.
        const box = document.createElement("div");
        box.className = "card-preview";
        preview.replaceWith(box);
        preview = box;

        if (kind === "image") {
            const img = document.createElement("img");
            img.src = href;
            img.alt = name;
            img.loading = "lazy";
            img.decoding = "async";
            preview.replaceChildren(img);
        } else if (kind === "video") {
            const vid = document.createElement("video");
            vid.preload = "metadata";
            const src = document.createElement("source");
            src.src = href;
            vid.appendChild(src);
            preview.replaceChildren(vid);
        } else {
            preview.classList.add("card-preview-audio");
            const audio = document.createElement("audio");
            audio.controls = true;
            audio.preload = "none";
            const src = document.createElement("source");
            src.src = href;
            audio.appendChild(src);
            preview.replaceChildren(audio);
        }

        if (kind !== "audio") card.appendChild(downloadOverlay(href));
    });
}

// ── Table view ────────────────────────────────────────
// Same entries as the card grid, server-rendered as <tr>s with empty type and
// size cells — filled in here from the same extension maps the cards use, and
// from the same byte formatter the upload toast uses.
function hydrateTable() {
    document.querySelectorAll(".file-row").forEach(row => {
        const name = row.dataset.name ?? "";
        const isDir = row.hasAttribute("data-dir");
        const ext = name.split(".").pop().toLowerCase();
        const inline = !isDir && (opensInline(ext) || ext in MEDIA_KIND || row.hasAttribute("data-text"));
        row.classList.toggle("file-row-inline", inline);
        row.querySelector(".row-icon").textContent = isDir ? "📁" : inline ? "📖" : (KIND_ICON[ext] ?? "📎");
        row.querySelector(".row-type").textContent = isDir ? "Folder" : fileTypeLabel(name);
        if (!isDir) {
            const dl = document.createElement("a");
            dl.className = "row-download";
            dl.href = row.dataset.href + "?dl=1";
            dl.download = "";
            dl.title = "Download";
            dl.textContent = "⭳";
            row.querySelector(".row-dl").appendChild(dl);
        }
        row.querySelector(".row-size").textContent = isDir ? "" : formatBytes(Number(row.dataset.size ?? 0));
    });
}

// ── Card / table view toggle ─────────────────────────────
function setFileView(view) {
    const list = $("file-list");
    if (!list) return;
    list.classList.remove("view-cards", "view-table");
    list.classList.add(view === "table" ? "view-table" : "view-cards");
    document.querySelectorAll("#view-picker .view-picker-btn").forEach(btn => {
        btn.classList.toggle("active", btn.dataset.view === view);
    });
    localStorage.setItem("fileshare-view", view);
}

function initViewPicker() {
    const picker = $("view-picker");
    if (!picker) return;
    picker.addEventListener("click", e => {
        const btn = e.target.closest(".view-picker-btn");
        if (btn) setFileView(btn.dataset.view);
    });
    setFileView(localStorage.getItem("fileshare-view") === "table" ? "table" : "cards");
}

// ── Filter & selection ────────────────────────────────
// Cards and rows are the same entries twice; both carry data-name, and a
// checkbox in either view marks the entry in both.
const selected = new Set();

function applyFilter(query) {
    const q = query.trim().toLowerCase();
    let shown = 0;
    document.querySelectorAll(".file-card, .file-row").forEach(el => {
        const hit = !q || (el.dataset.name ?? "").toLowerCase().includes(q);
        el.hidden = !hit;
        if (hit) shown++;
    });
    const empty = $("filter-empty");
    if (empty) empty.hidden = shown > 0;
}

function initFilter() {
    const input = $("file-filter");
    if (!input) return;
    input.addEventListener("input", () => applyFilter(input.value));
    input.addEventListener("keydown", e => {
        if (e.key === "Escape") { input.value = ""; applyFilter(""); }
    });
}

function setSelected(name, on) {
    selected[on ? "add" : "delete"](name);
    document.querySelectorAll(".select-box").forEach(box => {
        if (box.closest("[data-name]")?.dataset.name === name) box.checked = on;
    });
}

function updateZipButton() {
    const btn = $("zip-btn");
    if (btn) btn.textContent = selected.size ? `ZIP (${selected.size})` : "ZIP";
    const all = $("select-all");
    if (all) {
        const rows = document.querySelectorAll(".file-row:not([hidden])");
        const picked = [...rows].filter(r => selected.has(r.dataset.name)).length;
        all.checked = rows.length > 0 && picked === rows.length;
        all.indeterminate = picked > 0 && picked < rows.length;
    }
}

function initSelection() {
    document.addEventListener("change", e => {
        const box = e.target.closest(".select-box");
        if (!box) return;
        setSelected(box.closest("[data-name]").dataset.name, box.checked);
        updateZipButton();
    });
    $("select-all")?.addEventListener("change", e => {
        document.querySelectorAll(".file-row:not([hidden])").forEach(row => setSelected(row.dataset.name, e.target.checked));
        updateZipButton();
    });
}

function closeCrumbDropdowns(except) {
    document.querySelectorAll("#breadcrumb-nav .dropdown-panel.open").forEach(p => {
        if (p === except) return;
        p.classList.remove("open");
        p.parentElement.querySelector(".crumb-sep-btn")?.setAttribute("aria-expanded", "false");
    });
}

function initCrumbDropdowns() {
    const nav = $("breadcrumb-nav");
    if (!nav) return;

    nav.addEventListener("click", e => {
        const btn = e.target.closest(".crumb-sep-btn");
        if (!btn) return;
        e.preventDefault();
        const panel = btn.parentElement.querySelector(".dropdown-panel");
        const open = !panel.classList.contains("open");
        closeCrumbDropdowns(panel);
        panel.classList.toggle("open", open);
        btn.setAttribute("aria-expanded", String(open));
    });

    document.addEventListener("click", e => {
        if (!e.target.closest("#breadcrumb-nav .dropdown-wrap")) closeCrumbDropdowns();
    });
    document.addEventListener("keydown", e => {
        if (e.key === "Escape") closeCrumbDropdowns();
    });
}

function setupMediaContainer(container) {
    const media = container.querySelector("img, video");
    if (media) mediaElements.push(media);
    if (media) {
        container.addEventListener("click", () => openLightbox(media));
        if (media.tagName === "VIDEO")
            media.addEventListener("click", e => { e.stopPropagation(); openLightbox(media); });
    }
}

// ── ZIP ───────────────────────────────────────────────
function downloadAsZip(e) {
    e.preventDefault();
    const params = new URLSearchParams({ download: "zip" });
    selected.forEach(name => params.append("f", name));
    const a = Object.assign(document.createElement("a"), {
        href: location.origin + location.pathname + "?" + params,
        download: "archive.zip",
    });
    document.body.append(a);
    a.click();
    a.remove();
}

// ── Timestamps ────────────────────────────────────────
const fmtTs = ts => new Date(+ts * 1000).toLocaleString(navigator.language || "en-US", {
    weekday: "long", year: "2-digit", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
});

const _tsEl = $("upload-time");
if (_tsEl?.dataset.timestamp) _tsEl.innerText = fmtTs(_tsEl.dataset.timestamp);

const _timeLeftEl = $("time-left");
if (_timeLeftEl) {
    const diff = new Date(+_timeLeftEl.dataset.timestamp * 1000) - Date.now();
    _timeLeftEl.innerText = diff <= 0 ? "expired"
        : `${Math.floor(diff / 86400000)}d ${Math.floor(diff % 86400000 / 3600000)}h ${Math.floor(diff % 3600000 / 60000)}m`;
}

// ── Upload toast toggle ───────────────────────────────
let uploadToastOpen = false;

function toggleUploadToast() {
    uploadToastOpen = !uploadToastOpen;
    $("upload-toast")?.classList.toggle("visible", uploadToastOpen);
}

document.addEventListener("click", e => {
    if (!$("upload-badge")?.contains(e.target)) {
        uploadToastOpen = false;
        $("upload-toast")?.classList.remove("visible");

        const saved = localStorage.getItem("uploadResult");
        if (saved) {
            try {
                const data = JSON.parse(saved);
                data.toastWasOpen = false;
                localStorage.setItem("uploadResult", JSON.stringify(data));
            } catch { }
        }
    }
});

// ── Format helpers ────────────────────────────────────
const _UNITS = ["B", "KB", "MB", "GB", "TB"];
const _fmt = (b, sfx = "") => {
    let i = 0, n = b;
    while (n >= 1024 && i < _UNITS.length - 1) { n /= 1024; i++; }
    return `${i === 0 ? n : n.toFixed(1)} ${_UNITS[i]}${sfx}`;
};
const formatBytes = b => _fmt(b);
const formatSpeed = b => _fmt(b, "/s");

const formatEta = s => {
    if (!isFinite(s) || s <= 0) return null;
    const t = Math.ceil(s);
    return t < 60 ? `~${t}s remaining` : `~${Math.floor(t / 60)}m ${t % 60}s remaining`;
};

/* Chunk size for the upload: CHUNK_SIZE, or about two seconds' worth of MAX_UPLOAD_RATE when that is set and smaller. */
const uploadChunkSize = () => MAX_UPLOAD_RATE > 0
    ? Math.max(MIN_CHUNK_SIZE, Math.min(CHUNK_SIZE, Math.floor(MAX_UPLOAD_RATE * 2 / MIN_CHUNK_SIZE) * MIN_CHUNK_SIZE))
    : CHUNK_SIZE;

/* Token bucket over MAX_UPLOAD_RATE; resolves once n bytes may start, at once when no rate is set. */
const rateGate = { tokens: Infinity, last: 0 };
async function waitForRate(n) {
    if (!(MAX_UPLOAD_RATE > 0)) return;
    for (;;) {
        const now = performance.now();
        rateGate.tokens = Math.min(Math.max(n, MAX_UPLOAD_RATE), rateGate.tokens + (now - rateGate.last) / 1000 * MAX_UPLOAD_RATE);
        rateGate.last = now;
        if (rateGate.tokens >= n) { rateGate.tokens -= n; return; }
        await new Promise(r => setTimeout(r, Math.min(250, (n - rateGate.tokens) / MAX_UPLOAD_RATE * 1000)));
    }
}

/* Average speed over the bytes sent in this session and the active time; null until ETA_AFTER_MS have passed.
   @returns {{speed: number, eta: number} | null} bytes per second and seconds left */
function estimateUpload(sentBytes, remainingBytes, activeMs) {
    if (activeMs < ETA_AFTER_MS || sentBytes <= 0) return null;
    const speed = sentBytes / (activeMs / 1000);
    return { speed, eta: remainingBytes / speed };
}

const EXT_MAP = {
    jpg: "JPG", jpeg: "JPG", png: "PNG", gif: "GIF", webp: "WEBP", svg: "SVG",
    mp4: "MP4", mov: "MOV", avi: "AVI", webm: "WEBM", mp3: "MP3", wav: "WAV", ogg: "OGG",
    pdf: "PDF", zip: "ZIP", rar: "RAR", "7z": "7Z", tar: "TAR", gz: "GZ",
    doc: "DOC", docx: "DOCX", xls: "XLS", xlsx: "XLSX", ppt: "PPT", pptx: "PPTX",
    txt: "TXT", md: "MD", json: "JSON", csv: "CSV",
    js: "JS", ts: "TS", html: "HTML", css: "CSS", py: "PY",
};
const fileTypeLabel = name => {
    const ext = name.split(".").pop().toLowerCase();
    return (EXT_MAP[ext] ?? ext.toUpperCase().slice(0, 4)) || "FILE";
};

const _ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" };
const escapeHtml = s => s.replace(/[&<>"]/g, c => _ESC[c]);

// ── Upload state ──────────────────────────────────────
const uploadStats = { totalBytes: 0, bytesUploaded: 0, sentBytes: 0, activeMs: 0, activeSince: null };
const activeMs = () => uploadStats.activeMs + (uploadStats.activeSince ? Date.now() - uploadStats.activeSince : 0);
const stopClock = () => { uploadStats.activeMs = activeMs(); uploadStats.activeSince = null; };
let activeRun = null;
const newRun = () => ({ tasks: [], open: 0, sending: 0, waiters: [], cancelled: false });
const showPaused = () => {
    const sending = activeRun?.sending ?? 0;
    if (!sending) stopClock();
    $("upload-toast-eta").textContent = sending ? "Pausing…" : "Paused";
};
const wakeScheduler = (run = activeRun) => run?.waiters.splice(0).forEach(r => r());
const schedulerChange = run => new Promise(r => run.waiters.push(r));
let fileStates = [], uploadControlState = "idle", pauseResolvers = [], cancelReject = null;
const fileSkipFlags = {};
const fileXhrs = {}; // fileIndex -> Set<XMLHttpRequest>
const filePausedFlags = {};
const filePauseResolvers = {};

// ── Per-file controls ─────────────────────────────────
window.abortFile = i => {
    const f = fileStates[i];
    if (!f || f.status === "done" || f.status === "error" || f.status === "skipped" || f.status === "cancelled") return;
    if (filePausedFlags[i]) {
        delete filePausedFlags[i];
        (filePauseResolvers[i] ?? []).splice(0).forEach(r => r());
        delete filePauseResolvers[i];
    }
    fileXhrs[i]?.forEach(x => x.abort());
    delete fileXhrs[i];
    fileSkipFlags[i] = true;
    f.status = "skipped";
    f.skipped = true;
    renderFileList();
    wakeScheduler();
};

window.togglePauseFile = i => {
    const f = fileStates[i];
    if (!f || f.status !== "uploading") return;
    if (filePausedFlags[i]) {
        delete filePausedFlags[i];
        (filePauseResolvers[i] ?? []).splice(0).forEach(r => r());
        delete filePauseResolvers[i];
    } else {
        filePausedFlags[i] = true;
    }
    renderFileList();
    wakeScheduler();
};

// ── Global pause / resume ─────────────────────────────
function updatePauseButton() {
    const btn = $("toast-pause-btn");
    if (!btn) return;
    const paused = uploadControlState === "paused";
    btn.textContent = paused ? "▶" : "⏸";
    btn.title = paused ? "Resume all" : "Pause all";
    btn.classList.toggle("pause-active", paused);
    btn.style.display = uploadControlState === "idle" ? "none" : "";
}

const waitIfPaused = () =>
    uploadControlState !== "paused" ? Promise.resolve()
        : new Promise(r => pauseResolvers.push(r));

window.togglePauseUpload = function () {
    const dot = $q(".upload-badge-dot");
    if (uploadControlState === "uploading") {
        uploadControlState = "paused";
        if (dot) dot.style.animationPlayState = "paused";
        $("upload-toast-speed").textContent = "";
        showPaused();
        $q(".upload-toast-title").textContent = "Paused";
    } else if (uploadControlState === "paused") {
        uploadControlState = "uploading";
        uploadStats.activeSince ??= Date.now();
        pauseResolvers.splice(0).forEach(r => r());
        wakeScheduler();
        if (dot) dot.style.animationPlayState = "";
    }
    updatePauseButton();
};

window.abortAllUploads = function () {
    if (uploadControlState === "idle") return;
    uploadControlState = "cancelled";
    if (activeRun) activeRun.cancelled = true;
    cancelReject?.(new Error("cancelled"));
    fileStates.forEach((_, i) => abortFile(i));
    pauseResolvers.splice(0).forEach(r => r());
    wakeScheduler();
};

// ── File list rendering ───────────────────────────────
const FILE_STATUS = {
    done: ["--done", "done"],
    error: ["--error", "failed"],
    uploading: ["--uploading", null],
    skipped: ["--skipped", "skipped"],
    cancelled: ["--cancelled", "cancelled"],
};

function renderFileList() {
    const list = $("upload-toast-file-list");
    if (!list) return;
    list.innerHTML = fileStates.map((f, i) => {
        const [cls, txt] = FILE_STATUS[f.status] ?? ["--pending", "waiting"];
        const active = f.status === "uploading" || f.status === "pending";
        const paused = !!filePausedFlags[i];
        return `<div class="upload-file-row" data-index="${i}">
            <span class="upload-file-type">${fileTypeLabel(f.name)}</span>
            <div class="upload-file-info">
                <div class="upload-file-name-row">
                    <span class="upload-file-name">${escapeHtml(f.name)}</span>
                    <span class="upload-file-size">${formatBytes(f.size)}</span>
                </div>
                ${f.status === "uploading"
                ? `<div class="upload-file-minibar"><div class="upload-file-minibar-fill" style="width:${f.progress}%"></div></div>` : ""}
                ${f.error ? `<span class="upload-file-error">${escapeHtml(f.error)}</span>` : ""}
            </div>
            <span class="upload-file-status upload-file-status${cls}">${txt ?? `${f.progress}%`}</span>
            ${active ? `
            <div class="upload-file-actions">
                <button class="upload-file-btn upload-file-btn--pause${paused ? " is-paused" : ""}"
                    onclick="togglePauseFile(${i})" title="${paused ? "Resume" : "Pause"}">${paused ? "▶" : "⏸"}</button>
                <button class="upload-file-btn upload-file-btn--abort"
                    onclick="abortFile(${i})" title="Abort">✕</button>
            </div>` : ""}
        </div>`;
    }).join("");

    list.querySelectorAll(".upload-file-name").forEach(el => {
        el.classList.toggle("is-overflow", el.scrollWidth > el.clientWidth);
    });
}

// ── Stripe progress bar ───────────────────────────────
function stripe(pct, done) {
    const s = $("upload-stripe"), f = $("upload-stripe-fill");
    if (!s) return;
    f.style.width = pct + "%";
    if (done === undefined) {
        s.classList.add("active");
        s.classList.remove("done", "error");
        s.style.opacity = s.style.transition = "";
    } else {
        s.classList.remove("active");
        s.classList.add(done ? "done" : "error");
        setTimeout(() => { s.style.transition = "opacity .6s ease"; s.style.opacity = "0"; }, 900);
    }
}

// ── Upload progress UI ────────────────────────────────
function setUploadProgress(pct, loaded, total) {
    if (!$("upload-badge")) return;
    $("upload-badge").classList.add("visible");
    const bt = $("upload-badge-text"), tp = $("upload-toast-pct");
    if (bt) bt.textContent = pct + "%";
    if (tp) tp.textContent = pct + "%";
    stripe(pct);

    if (uploadControlState !== "uploading" || loaded == null) return;

    const est = estimateUpload(uploadStats.sentBytes, total - loaded, activeMs());
    const ts = $("upload-toast-speed"); if (ts) ts.textContent = est ? formatSpeed(est.speed) : "";
    const te = $("upload-toast-eta"); if (te) te.textContent = est ? formatEta(est.eta) ?? "" : "";
}

// ── Shared end-of-upload state reset ─────────────────
function _finalize(dotColor) {
    const dot = $q(".upload-badge-dot");
    if (dot) { dot.style.animation = "none"; dot.style.background = dotColor; }
    uploadControlState = "idle";
    cancelReject = null;
    updatePauseButton();
    const ts = $("upload-toast-speed"); if (ts) ts.textContent = "";
    const te = $("upload-toast-eta"); if (te) te.textContent = "";
}

function finishUploadProgress(succeeded, failed, totalBytes, durationMs) {
    const ok = failed === 0;
    _finalize(ok ? "var(--color-success)" : "var(--color-warning)");

    const tp = $("upload-toast-pct");
    if (tp) { tp.textContent = "100%"; tp.classList.add("done"); }

    const bt = $("upload-badge-text"), tt = $q(".upload-toast-title");
    const te = $("upload-toast-eta"), sub = $("upload-toast-sub");
    if (bt) bt.textContent = failed > 0 ? `${succeeded}/${succeeded + failed}` : "Done";
    if (tt) tt.textContent = failed > 0 ? `${succeeded} uploaded, ${failed} failed` : "Upload complete";
    if (te && durationMs) te.textContent = `Completed in ${(durationMs / 1000).toFixed(1)}s`;
    if (sub && totalBytes && durationMs)
        sub.textContent = `${formatBytes(totalBytes)} · avg ${formatSpeed(totalBytes / (durationMs / 1000))}`;

    stripe(100, ok);
    fileStates.forEach(f => { if (f.status !== "error" && f.status !== "skipped") { f.status = "done"; f.progress = 100; } });
    renderFileList();

    localStorage.setItem("uploadResult", JSON.stringify(
        { succeeded, failed, ok, toastWasOpen: uploadToastOpen, totalBytes, durationMs, fileStates }
    ));
    setTimeout(() => location.reload(), 2500);
}

function handleUploadError(reason) {
    _finalize("var(--color-error)");
    const bt = $("upload-badge-text"), tt = $q(".upload-toast-title"),
        te = $("upload-toast-eta");
    if (bt) bt.textContent = "Error";
    if (tt) tt.textContent = `Upload failed: ${reason}`;
    if (te) te.textContent = "";

    stripe(100, false);
    fileStates.forEach(f => { if (f.status === "uploading" || f.status === "pending") { f.status = "error"; f.error = reason; } });
    renderFileList();

    uploadToastOpen = true;
    $("upload-toast")?.classList.add("visible");
    $("uploadButtonText").textContent = "Upload";
}

// ── Chunked upload core ───────────────────────────────
/* Posts one chunk as the raw request body; create carries filename, size and chunkSize for a file sent without chunk-init.
   @returns {{sent: Promise<void>, done: Promise<void>}} sent settles once the body is out, done with the server's answer */
function sendChunk(base, uploadId, index, blob, onProgress, fileIndex, create) {
    const xhr = new XMLHttpRequest();
    (fileXhrs[fileIndex] ??= new Set()).add(xhr);
    const cleanup = () => fileXhrs[fileIndex]?.delete(xhr);
    let markSent;
    const sent = new Promise(r => { markSent = r; });
    const done = new Promise((resolve, reject) => {
        xhr.upload.onprogress = e => { if (e.lengthComputable) onProgress(e.loaded); };
        xhr.upload.onload = () => markSent();
        xhr.onload = () => {
            cleanup(); markSent();
            (xhr.status === 202 || xhr.status === 204) ? resolve() : reject(new Error(`chunk ${index}: HTTP ${xhr.status}`));
        };
        xhr.onerror = () => { cleanup(); markSent(); reject(new Error("network error")); };
        xhr.onabort = () => { cleanup(); markSent(); resolve(); };
    });
    const query = new URLSearchParams({ uploadId, chunkIndex: index, ...create });
    xhr.open("POST", `${base}/chunk?${query}`);
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.send(blob);
    return { sent, done };
}

async function computeUploadHash(file, chunkSize) {
    const input = `${file.name}:${file.size}:${file.lastModified}:${chunkSize}`;
    let h1 = 0xdeadbeef ^ input.length, h2 = 0x41c6ce57 ^ input.length;
    for (let i = 0; i < input.length; i++) {
        const ch = input.charCodeAt(i);
        h1 = Math.imul(h1 ^ ch, 2654435761);
        h2 = Math.imul(h2 ^ ch, 1597334677);
    }
    h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909);
    h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909);
    return (h1 >>> 0).toString(16).padStart(8, "0") + (h2 >>> 0).toString(16).padStart(8, "0");
}

const chunkLength = (file, chunkSize, index) => Math.min(chunkSize, file.size - index * chunkSize);

/* Asks the server which chunks of file it still needs; a file that fits into one chunk is sent without asking.
   @returns {Promise<{uploadId: string, chunkSize: number, missing: number[], storedBytes: number, create?: object}>} */
async function initUpload(file, base) {
    const chunkSize = uploadChunkSize();
    const uploadId = await computeUploadHash(file, chunkSize);
    if (file.size <= chunkSize) {
        return { uploadId, chunkSize, missing: [0], storedBytes: 0, create: { filename: file.name, size: file.size, chunkSize } };
    }
    const resp = await fetch(`${base}/chunk-init`, {
        method: "POST",
        body: new URLSearchParams({ uploadId, filename: file.name, size: file.size, chunkSize }),
    });
    if (!resp.ok) throw new Error(`init failed: HTTP ${resp.status}`);
    const { missingChunks } = await resp.json();
    const missingBytes = missingChunks.reduce((sum, index) => sum + chunkLength(file, chunkSize, index), 0);
    return { uploadId, chunkSize, missing: missingChunks, storedBytes: file.size - missingBytes };
}

/* Uploads all files of run through one queue of chunks, MAX_SENDING bodies at a time and at most MAX_OPEN requests open.
   ui.start(i), ui.bytes(i, n, sent) and ui.end(i, err) report progress; sent is false for chunks stored earlier. */
async function runUploads(run, files, base, ui) {
    const left = files.map(() => 0);
    const settledFiles = new Set();
    const finish = (i, err) => { if (!settledFiles.has(i)) { settledFiles.add(i); ui.end(i, err); } };
    const dropped = i => run.cancelled || fileSkipFlags[i] || settledFiles.has(i);
    const runnable = () => run.tasks.filter(t => !dropped(t.i) && !filePausedFlags[t.i]).length;
    let fed = false;

    const inits = [];
    const startInit = i => inits[i] ??= initUpload(files[i], base).then(r => ({ r }), err => ({ err }));
    const feeder = (async () => {
        for (let i = 0; i < files.length && !run.cancelled; i++) {
            while (runnable() >= QUEUE_LOW && !run.cancelled) await schedulerChange(run);
            for (let k = i; k < Math.min(files.length, i + INIT_AHEAD); k++) if (!fileSkipFlags[k]) startInit(k);
            if (run.cancelled) break;
            if (fileSkipFlags[i]) { finish(i, null); continue; }
            ui.start(i);
            const { r, err } = await startInit(i);
            if (run.cancelled) break;
            if (err || fileSkipFlags[i]) {
                finish(i, err ?? null);
            } else {
                if (r.storedBytes > 0) ui.bytes(i, r.storedBytes, false);
                left[i] = r.missing.length;
                if (!r.missing.length) finish(i, null);
                r.missing.forEach(index => run.tasks.push({ i, file: files[i], index, uploadId: r.uploadId, chunkSize: r.chunkSize, create: r.create }));
            }
            wakeScheduler(run);
        }
        fed = true;
        wakeScheduler(run);
    })();

    const nextTask = async () => {
        for (;;) {
            if (run.cancelled) return null;
            await waitIfPaused();
            if (run.cancelled) return null;
            run.tasks = run.tasks.filter(t => !dropped(t.i));
            const k = run.tasks.findIndex(t => !filePausedFlags[t.i]);
            if (k >= 0 && run.open < MAX_OPEN) {
                const task = run.tasks.splice(k, 1)[0];
                run.open++;
                wakeScheduler(run);
                return task;
            }
            if (fed && !run.tasks.length) return null;
            await schedulerChange(run);
        }
    };

    const pending = new Set();
    const worker = async () => {
        for (let task; (task = await nextTask());) {
            const { i, file, index, uploadId, chunkSize, create } = task;
            const start = index * chunkSize;
            const blob = file.slice(start, start + chunkLength(file, chunkSize, index));
            await waitForRate(blob.size);
            if (dropped(i) || uploadControlState === "paused" || filePausedFlags[i]) {
                if (!dropped(i)) run.tasks.unshift(task);
                run.open--;
                wakeScheduler(run);
                continue;
            }
            let reported = 0;
            run.sending++;
            const { sent, done } = sendChunk(base, uploadId, index, blob, loaded => {
                if (!dropped(i)) ui.bytes(i, loaded - reported, true);
                reported = loaded;
            }, i, create);
            const settled = done.then(() => {
                if (dropped(i)) return;
                ui.bytes(i, blob.size - reported, true);
                if (--left[i] === 0) finish(i, null);
            }, err => {
                if (dropped(i)) return;
                finish(i, err);
                fileXhrs[i]?.forEach(x => x.abort());
            }).finally(() => {
                run.open--;
                pending.delete(settled);
                wakeScheduler(run);
            });
            pending.add(settled);
            await sent;
            run.sending--;
            if (run === activeRun && uploadControlState === "paused") showPaused();
        }
    };

    await Promise.all([feeder, ...Array.from({ length: MAX_SENDING }, worker)]);
    while (pending.size) await Promise.all([...pending]);
}

// ── Init ──────────────────────────────────────────────
document.addEventListener("DOMContentLoaded", () => {
    // Cards first; hydrateCards adds .media-container to previewable ones.
    hydrateCards();
    document.querySelectorAll(".media-container").forEach(setupMediaContainer);
    hydrateTable();
    initViewPicker();
    initCrumbDropdowns();
    initFilter();
    initSelection();
    initTextPreview();

    $("lightbox-close")?.addEventListener("click", closeLightbox);
    $("lightbox")?.addEventListener("click", e => { if (e.target === e.currentTarget) closeLightbox(); });
    document.addEventListener("keydown", e => {
        if (!isLightboxOpen) return;
        if (e.key === "Escape") closeLightbox();
        else if (e.key === "ArrowRight") navigateLightbox(1);
        else if (e.key === "ArrowLeft") navigateLightbox(-1);
    });

    updatePauseButton();

    // ── Restore post-reload toast state ──────────────
    const saved = localStorage.getItem("uploadResult");
    if (saved) {
        localStorage.removeItem("uploadResult");
        const { succeeded, failed, ok, toastWasOpen, totalBytes, durationMs, fileStates: sf } = JSON.parse(saved);
        if (sf) { fileStates = sf; renderFileList(); }

        const dot = $q(".upload-badge-dot");
        const color = ok ? "var(--color-success)" : "var(--color-warning)";
        const tp = $("upload-toast-pct"), tt = $q(".upload-toast-title"),
            bt = $("upload-badge-text"), sub = $("upload-toast-sub"),
            te = $("upload-toast-eta");

        if (tp) { tp.textContent = "100%"; tp.classList.add("done"); }
        if (tt) tt.textContent = failed > 0 ? `${succeeded} uploaded, ${failed} failed` : "Upload complete";
        if (bt) bt.textContent = failed > 0 ? `${succeeded}/${succeeded + failed}` : "Done";
        if (dot) { dot.style.animation = "none"; dot.style.background = color; }
        if (sub && totalBytes && durationMs)
            sub.textContent = `${formatBytes(totalBytes)} · avg ${formatSpeed(totalBytes / (durationMs / 1000))}`;
        if (te && durationMs) te.textContent = `Completed in ${(durationMs / 1000).toFixed(1)}s`;

        $("upload-badge")?.classList.add("visible");
        uploadToastOpen = toastWasOpen ?? false;
        if (uploadToastOpen) {
            $("upload-toast")?.classList.add("visible");
            setTimeout(() => { uploadToastOpen = false; $("upload-toast")?.classList.remove("visible"); }, 4000);
        }
    }

    // ── Upload handler ────────────────────────────────
    const subpath = location.pathname.split("/").filter(Boolean)[0] ?? "";
    const chunkBase = `${location.origin}/${subpath}`;

    window.submitUpload = async function () {
        if (uploadControlState !== "idle" || !$("uploadForm")) return;
        const files = Array.from($("fileUpload").files);
        if (!files.length) return;

        let totalBytes = 0;
        fileStates = files.map(f => { totalBytes += f.size; return { name: f.name, size: f.size, status: "pending", progress: 0, error: null, skipped: false }; });
        Object.keys(fileSkipFlags).forEach(k => delete fileSkipFlags[k]);
        Object.keys(filePausedFlags).forEach(k => delete filePausedFlags[k]);
        Object.keys(filePauseResolvers).forEach(k => delete filePauseResolvers[k]);

        Object.assign(uploadStats, { totalBytes, bytesUploaded: 0, sentBytes: 0, activeMs: 0, activeSince: Date.now() });
        uploadControlState = "uploading";
        pauseResolvers = [];
        updatePauseButton();

        $q(".upload-toast-title").textContent = `Uploading ${files.length} file${files.length !== 1 ? "s" : ""}…`;
        setUploadProgress(0, 0, totalBytes);
        $("uploadButtonText").textContent = "Uploading…";
        if (!uploadToastOpen) { uploadToastOpen = true; $("upload-toast")?.classList.add("visible"); }

        await fetch("/api/log", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ message: "Upload initiated" }),
        });

        let succeeded = 0, failed = 0;

        const fileBytes = files.map(() => 0);
        let drawPending = false;
        const run = newRun();
        activeRun = run;
        const ui = {
            start(i) {
                if (run.cancelled) return;
                fileStates[i].status = "uploading";
                renderFileList();
            },
            bytes(i, n, sent) {
                if (run.cancelled) return;
                fileBytes[i] += n;
                uploadStats.bytesUploaded += n;
                if (sent) uploadStats.sentBytes += n;
                fileStates[i].progress = files[i].size ? Math.round(fileBytes[i] / files[i].size * 100) : 100;
                if (!drawPending) {
                    drawPending = true;
                    requestAnimationFrame(() => {
                        drawPending = false;
                        if (run.cancelled) return;
                        setUploadProgress(Math.round(uploadStats.bytesUploaded / totalBytes * 100), uploadStats.bytesUploaded, totalBytes);
                        renderFileList();
                    });
                }
            },
            end(i, err) {
                if (run.cancelled) return;
                if (fileSkipFlags[i]) {
                    fileStates[i].status = "skipped";
                    fileStates[i].skipped = true;
                } else if (err) {
                    fileStates[i].status = "error";
                    fileStates[i].error = err.message;
                    failed++;
                } else {
                    fileStates[i].status = "done";
                    fileStates[i].progress = 100;
                    succeeded++;
                }
                renderFileList();
            },
        };

        try {
            await new Promise((resolve, reject) => {
                cancelReject = reject;
                runUploads(run, files, chunkBase, ui).then(resolve, reject);
            });
        } catch {
            _finalize("var(--text-faint)");
            const bt = $("upload-badge-text"); if (bt) bt.textContent = "—";
            $q(".upload-toast-title").textContent = "Upload cancelled — chunks saved for resume";
            fileStates.forEach(f => {
                if (f.status === "uploading" || f.status === "pending")
                    f.status = "cancelled";
            });
            renderFileList();
            stripe(100, false);
            $("uploadButtonText").textContent = "Upload";
            return;
        }

        cancelReject = null;
        stopClock();
        const durationMs = uploadStats.activeMs;
        if (succeeded === 0 && failed > 0)
            handleUploadError(`All ${failed} file${failed !== 1 ? "s" : ""} failed`);
        else
            finishUploadProgress(succeeded, failed, totalBytes, durationMs);
    };
});

(function () {
    if (!document.getElementById('fileUpload')) return;

    const dropZone = document.querySelector('.app-shell');

    const breadcrumb = document.getElementById('breadcrumb');
    const rect = breadcrumb.getBoundingClientRect();

    const hitArea = document.createElement('div');
    hitArea.style.cssText = `position:fixed;top:${rect.bottom}px;left:${rect.left}px;right:${window.innerWidth - rect.right}px;bottom:0;z-index:499;pointer-events:none;`;
    document.body.appendChild(hitArea);

    function updateHitArea() {
        const r = breadcrumb.getBoundingClientRect();
        hitArea.style.top = `${r.bottom}px`;
        hitArea.style.left = `${r.left}px`;
        hitArea.style.right = `${window.innerWidth - r.right}px`;
    }

    document.addEventListener('dragover', (e) => {
        if (!e.dataTransfer.types.includes('Files')) return;
        hitArea.style.pointerEvents = 'all';
    });
    document.addEventListener('dragleave', (e) => {
        if (e.relatedTarget === null) hitArea.style.pointerEvents = 'none';
    });

    hitArea.addEventListener('dragenter', (e) => {
        e.preventDefault();
        updateHitArea();
        dropZone.style.setProperty('--drag-top', hitArea.style.top);
        dropZone.style.setProperty('--drag-left', hitArea.style.left);
        dropZone.style.setProperty('--drag-right', hitArea.style.right);
        dropZone.classList.add('drag-over');
    });
    hitArea.addEventListener('dragover', (e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'copy';
    });
    hitArea.addEventListener('dragleave', () => {
        dropZone.classList.remove('drag-over');
        hitArea.style.pointerEvents = 'none';
    });
    hitArea.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        hitArea.style.pointerEvents = 'none';
        const files = e.dataTransfer.files;
        if (!files.length) return;

        const input = document.getElementById('fileUpload');
        const dt = new DataTransfer();
        for (const file of files) dt.items.add(file);
        input.files = dt.files;
        submitUpload();
    });
    document.addEventListener('dragend', () => {
        dropZone.classList.remove('drag-over');
        hitArea.style.pointerEvents = 'none';
    });
})();