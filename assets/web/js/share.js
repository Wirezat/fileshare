// ── Shorthands & constants ────────────────────────────
const _dc = {};
const $ = id => _dc[id] || (_dc[id] = document.getElementById(id));
const $q = s => document.querySelector(s);

const CHUNK_SIZE = 5 * 1024 * 1024;
const MAX_PARALLEL = 4;

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
    img.style.display = vid.style.display = "none";
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

function navigateLightbox(dir) {
    if (!mediaElements.length) return;
    currentMediaIndex = (currentMediaIndex + dir + mediaElements.length) % mediaElements.length;
    openLightbox(mediaElements[currentMediaIndex]);
}

function setupMediaContainer(container) {
    const overlay = container.querySelector(".overlay");
    const media = container.querySelector("img, video");
    if (media) mediaElements.push(media);
    container.addEventListener("mouseenter", () => overlay && (overlay.style.display = "flex"));
    container.addEventListener("mouseleave", () => overlay && (overlay.style.display = "none"));
    overlay?.querySelector(".download-button")?.addEventListener("click", e => e.stopPropagation());
    if (media) {
        container.addEventListener("click", () => openLightbox(media));
        if (media.tagName === "VIDEO")
            media.addEventListener("click", e => { e.stopPropagation(); openLightbox(media); });
    }
}

// ── ZIP ───────────────────────────────────────────────
function downloadAsZip(e) {
    e.preventDefault();
    const a = Object.assign(document.createElement("a"), {
        href: location.origin + location.pathname + "?download=zip",
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

const _usesEl = $("uses");
if (_usesEl?.dataset.uses != null) _usesEl.innerText = _usesEl.dataset.uses;

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
const _fmt = (b, sfx = "") =>
    b < 1024 ? `${b} B${sfx}`
        : b < 1048576 ? `${(b / 1024).toFixed(1)} KB${sfx}`
            : `${(b / 1048576).toFixed(1)} MB${sfx}`;
const formatBytes = b => _fmt(b);
const formatSpeed = b => _fmt(b, "/s");

const formatEta = s => !isFinite(s) || s <= 0 ? null
    : s < 60 ? `~${Math.ceil(s)}s remaining`
        : `~${Math.floor(s / 60)}m ${Math.ceil(s % 60)}s remaining`;

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
const uploadStats = { totalBytes: 0, startTime: null, bytesUploaded: 0, lastSnapshot: null };
let fileStates = [], uploadControlState = "idle", pauseResolvers = [], cancelReject = null;
const fileSkipFlags = {};
const fileXhrs = {}; // fileIndex -> Set<XMLHttpRequest>
// Per-file pause: set of paused file indices
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
};

const waitIfFilePaused = i =>
    !filePausedFlags[i] ? Promise.resolve()
        : new Promise(r => {
            filePauseResolvers[i] = filePauseResolvers[i] ?? [];
            filePauseResolvers[i].push(r);
        });

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
        $("upload-toast-eta").textContent = "Paused";
        $q(".upload-toast-title").textContent = "Paused";
    } else if (uploadControlState === "paused") {
        uploadControlState = "uploading";
        pauseResolvers.splice(0).forEach(r => r());
        if (dot) dot.style.animationPlayState = "";
    }
    updatePauseButton();
};

window.abortAllUploads = function () {
    if (uploadControlState === "idle") return;
    uploadControlState = "cancelled";
    cancelReject?.(new Error("cancelled")); // erst rejecten, bevor Worker resolve() erreichen können
    fileStates.forEach((_, i) => abortFile(i));
    pauseResolvers.splice(0).forEach(r => r());
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

    const now = Date.now(), snap = uploadStats.lastSnapshot;
    if (snap) {
        const dt = (now - snap.time) / 1000, dl = loaded - snap.bytes;
        if (dt > 0.1) {
            const spd = dl / dt;
            const ts = $("upload-toast-speed"); if (ts) ts.textContent = formatSpeed(spd);
            const te = $("upload-toast-eta"); if (te) te.textContent = formatEta(spd > 0 ? (total - loaded) / spd : Infinity) ?? "";
            uploadStats.lastSnapshot = { bytes: loaded, time: now };
        }
    } else {
        uploadStats.lastSnapshot = { bytes: loaded, time: now };
    }
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
function sendChunk(base, uploadId, index, blob, onProgress, fileIndex) {
    return new Promise((resolve, reject) => {
        const fd = new FormData();
        fd.append("uploadId", uploadId);
        fd.append("chunkIndex", index);
        fd.append("chunk", blob);
        const xhr = new XMLHttpRequest();
        if (!fileXhrs[fileIndex]) fileXhrs[fileIndex] = new Set();
        fileXhrs[fileIndex].add(xhr);
        const cleanup = () => fileXhrs[fileIndex]?.delete(xhr);
        xhr.upload.onprogress = e => {
            if (e.lengthComputable) onProgress(e.loaded);
        };
        xhr.onload = () => { cleanup(); (xhr.status === 202 || xhr.status === 204) ? resolve() : reject(new Error(`chunk ${index}: HTTP ${xhr.status}`)); };
        xhr.onerror = () => { cleanup(); reject(new Error("network error")); };
        xhr.onabort = () => { cleanup(); resolve(); }; // Worker prüft danach selbst fileSkipFlags
        xhr.open("POST", `${base}/chunk`);
        xhr.send(fd);
    });
}

async function computeUploadHash(file) {
    const buf = await crypto.subtle.digest("SHA-256",
        new TextEncoder().encode(`${file.name}:${file.size}:${file.lastModified}`)
    );
    return Array.from(new Uint8Array(buf), b => b.toString(16).padStart(2, "0")).join("").slice(0, 32);
}

async function uploadFileChunked(fileIndex, file, base, onChunkDone) {
    const totalChunks = Math.ceil(file.size / CHUNK_SIZE) || 1;
    const uploadId = await computeUploadHash(file);

    const initResp = await fetch(`${base}/chunk-init`, {
        method: "POST",
        body: new URLSearchParams({ uploadId, filename: file.name, totalChunks }),
    });
    if (!initResp.ok) throw new Error(`init failed: HTTP ${initResp.status}`);
    const { missingChunks } = await initResp.json();

    const alreadyDone = totalChunks - missingChunks.length;
    if (alreadyDone > 0) {
        const lastSize = file.size % CHUNK_SIZE || CHUNK_SIZE;
        onChunkDone((alreadyDone - 1) * CHUNK_SIZE + (alreadyDone === totalChunks ? lastSize : CHUNK_SIZE));
    }

    let next = 0;
    const worker = async () => {
        while (next < missingChunks.length) {
            if (fileSkipFlags[fileIndex]) return;
            await waitIfPaused();
            await waitIfFilePaused(fileIndex);
            if (uploadControlState === "cancelled" || fileSkipFlags[fileIndex]) return;

            const index = missingChunks[next++];
            const start = index * CHUNK_SIZE;
            const blob = file.slice(start, Math.min(start + CHUNK_SIZE, file.size));

            let lastReported = 0;
            await sendChunk(base, uploadId, index, blob, loaded => {
                onChunkDone(loaded - lastReported);
                lastReported = loaded;
            }, fileIndex);

            if (uploadControlState === "cancelled" || fileSkipFlags[fileIndex]) return;
            onChunkDone(blob.size - lastReported);
        }
    };
    await Promise.all(Array.from({ length: Math.min(MAX_PARALLEL, missingChunks.length || 1) }, worker));
}

// ── Init ──────────────────────────────────────────────
document.addEventListener("DOMContentLoaded", () => {
    new LazyLoad({
        elements_selector: ".media-container img[data-src]",
        callback_enter: () => !isLightboxOpen,
        load_delay: 300,
    });
    document.querySelectorAll(".media-container").forEach(setupMediaContainer);

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

        Object.assign(uploadStats, { totalBytes, startTime: Date.now(), bytesUploaded: 0, lastSnapshot: null });
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

        try {
            await new Promise(async (resolve, reject) => {
                cancelReject = reject;
                for (let i = 0; i < files.length; i++) {
                    if (uploadControlState === "cancelled") break;
                    if (fileSkipFlags[i]) {
                        fileStates[i].status = "skipped";
                        fileStates[i].skipped = true;
                        renderFileList();
                        continue;
                    }

                    fileStates[i].status = "uploading";
                    let fileBytesUploaded = 0;
                    renderFileList();

                    try {
                        await uploadFileChunked(i, files[i], chunkBase, chunkBytes => {
                            if (uploadControlState !== "uploading") return; // kein UI-Update nach Cancel
                            fileBytesUploaded += chunkBytes;
                            uploadStats.bytesUploaded += chunkBytes;
                            fileStates[i].progress = Math.round(fileBytesUploaded / files[i].size * 100);
                            setUploadProgress(Math.round(uploadStats.bytesUploaded / totalBytes * 100), uploadStats.bytesUploaded, totalBytes);
                            renderFileList();
                        });

                        if (uploadControlState === "cancelled") break;
                        if (fileSkipFlags[i]) { fileStates[i].status = "skipped"; fileStates[i].skipped = true; }
                        else { fileStates[i].status = "done"; fileStates[i].progress = 100; succeeded++; }
                    } catch (err) {
                        if (uploadControlState === "cancelled") break;
                        fileStates[i].status = "error";
                        fileStates[i].error = err.message;
                        failed++;
                    }
                    renderFileList();
                }
                resolve();
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
        const durationMs = Date.now() - uploadStats.startTime;
        if (succeeded === 0 && failed > 0)
            handleUploadError(`All ${failed} file${failed !== 1 ? "s" : ""} failed`);
        else
            finishUploadProgress(succeeded, failed, totalBytes, durationMs);
    };
});

(function () {
    if (!document.getElementById('fileUpload')) return;

    const dropZone = document.querySelector('.container');

    function onDragEnter(e) {
        e.preventDefault();
        if (!e.dataTransfer.types.includes('Files')) return;
        dropZone.classList.add('drag-over');
    }

    function onDragOver(e) {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'copy';
    }

    function onDragLeave(e) {
        if (dropZone.contains(e.relatedTarget)) return;
        dropZone.classList.remove('drag-over');
    }

    function onDrop(e) {
        e.preventDefault();
        dropZone.classList.remove('drag-over');

        const files = e.dataTransfer.files;
        if (!files.length) return;

        const input = document.getElementById('fileUpload');
        const dt = new DataTransfer();
        for (const file of files) dt.items.add(file);
        input.files = dt.files;
        submitUpload();
    }

    document.addEventListener('dragend', () => dropZone.classList.remove('drag-over'));

    dropZone.addEventListener('dragenter', onDragEnter);
    dropZone.addEventListener('dragover', onDragOver);
    dropZone.addEventListener('dragleave', onDragLeave);
    dropZone.addEventListener('drop', onDrop);
})();