const DISPLAY = { FLEX: "flex", BLOCK: "block", NONE: "none" };

// ── Dark mode ────────────────────────────────────────
function toggleTheme() {
    const isDark = document.documentElement.classList.toggle('dark');
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
    document.getElementById('theme-toggle').textContent = isDark ? '☀️' : '🌙';
}
document.getElementById('theme-toggle').textContent =
    document.documentElement.classList.contains('dark') ? '☀️' : '🌙';

// ── Lightbox ─────────────────────────────────────────
let isLightboxOpen = false;
let currentMediaIndex = 0;
const mediaElements = [];

function setDisplay(el, v) { el.style.display = v; }

function openLightbox(mediaElement) {
    isLightboxOpen = true;
    const lb = document.getElementById("lightbox");
    const img = document.getElementById("lightbox-img");
    const vid = document.getElementById("lightbox-video");
    setDisplay(img, DISPLAY.NONE);
    setDisplay(vid, DISPLAY.NONE);
    currentMediaIndex = mediaElements.findIndex(el => el === mediaElement);
    if (mediaElement.tagName === "IMG") {
        img.src = mediaElement.src;
        setDisplay(img, DISPLAY.BLOCK);
    } else if (mediaElement.tagName === "VIDEO") {
        const src = vid.querySelector("source");
        src.src = mediaElement.querySelector("source").src;
        vid.load();
        setDisplay(vid, DISPLAY.BLOCK);
    }
    setDisplay(lb, DISPLAY.BLOCK);
    document.body.style.overflow = "hidden";
}

function closeLightbox() {
    isLightboxOpen = false;
    const vid = document.getElementById("lightbox-video");
    vid.pause();
    setDisplay(document.getElementById("lightbox"), DISPLAY.NONE);
    document.body.style.overflow = "";
}

function navigateLightbox(dir) {
    if (!mediaElements.length) return;
    currentMediaIndex = (currentMediaIndex + dir + mediaElements.length) % mediaElements.length;
    openLightbox(mediaElements[currentMediaIndex]);
}

function setupMediaContainer(container) {
    const overlay = container.querySelector(".overlay");
    const dlBtn = overlay?.querySelector(".download-button");
    const media = container.querySelector("img, video");
    if (media) mediaElements.push(media);

    container.addEventListener("mouseenter", () => { if (overlay) setDisplay(overlay, DISPLAY.FLEX); });
    container.addEventListener("mouseleave", () => { if (overlay) setDisplay(overlay, DISPLAY.NONE); });
    if (dlBtn) dlBtn.addEventListener("click", e => e.stopPropagation());

    if (media) {
        container.addEventListener("click", () => openLightbox(media));
        if (media.tagName === "VIDEO") {
            media.addEventListener("click", e => { e.stopPropagation(); openLightbox(media); });
        }
    }
}

// ── ZIP ──────────────────────────────────────────────
function downloadAsZip(event) {
    event.preventDefault();
    const link = document.createElement("a");
    link.href = window.location.origin + window.location.pathname + "?download=zip";
    link.download = "archive.zip";
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
}

// ── Timestamps ───────────────────────────────────────
function formatTimestamp(ts) {
    const date = new Date(Number(ts) * 1000);
    return date.toLocaleString(navigator.language || "en-US", {
        weekday: "long", year: "2-digit", month: "2-digit",
        day: "2-digit", hour: "2-digit", minute: "2-digit"
    });
}

const uploadTimeEl = document.getElementById("upload-time");
const ts = uploadTimeEl?.dataset.timestamp;
if (ts) uploadTimeEl.innerText = formatTimestamp(ts);

const timeLeftEl = document.getElementById("time-left");
if (timeLeftEl) {
    const exp = timeLeftEl.dataset.timestamp;
    if (exp) {
        const diff = new Date(Number(exp) * 1000) - new Date();
        if (diff <= 0) {
            timeLeftEl.innerText = "expired";
        } else {
            const d = Math.floor(diff / 86400000);
            const h = Math.floor((diff % 86400000) / 3600000);
            const m = Math.floor((diff % 3600000) / 60000);
            timeLeftEl.innerText = `${d}d ${h}h ${m}m`;
        }
    }
}

const usesEl = document.getElementById("uses");
if (usesEl?.dataset.uses !== undefined) usesEl.innerText = usesEl.dataset.uses;

// ── Upload Toast ──────────────────────────────────────
let uploadToastOpen = false;

function toggleUploadToast() {
    uploadToastOpen = !uploadToastOpen;
    document.getElementById("upload-toast")?.classList.toggle("visible", uploadToastOpen);
}

document.addEventListener("click", e => {
    const badge = document.getElementById("upload-badge");
    if (badge && !badge.contains(e.target)) {
        uploadToastOpen = false;
        document.getElementById("upload-toast")?.classList.remove("visible");
    }
});

// ── Format helpers ────────────────────────────────────
function formatBytes(bytes) {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatSpeed(bytesPerSec) {
    if (bytesPerSec < 1024) return `${bytesPerSec.toFixed(0)} B/s`;
    if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
    return `${(bytesPerSec / (1024 * 1024)).toFixed(1)} MB/s`;
}

function formatEta(seconds) {
    if (!isFinite(seconds) || seconds <= 0) return null;
    if (seconds < 60) return `~${Math.ceil(seconds)}s remaining`;
    const m = Math.floor(seconds / 60);
    const s = Math.ceil(seconds % 60);
    return `~${m}m ${s}s remaining`;
}

function fileTypeLabel(name) {
    const ext = name.split('.').pop().toLowerCase();
    const map = {
        jpg: "JPG", jpeg: "JPG", png: "PNG", gif: "GIF", webp: "WEBP", svg: "SVG",
        mp4: "MP4", mov: "MOV", avi: "AVI", webm: "WEBM",
        mp3: "MP3", wav: "WAV", ogg: "OGG",
        pdf: "PDF",
        zip: "ZIP", rar: "RAR", "7z": "7Z", tar: "TAR", gz: "GZ",
        doc: "DOC", docx: "DOCX", xls: "XLS", xlsx: "XLSX",
        ppt: "PPT", pptx: "PPTX",
        txt: "TXT", md: "MD", json: "JSON", csv: "CSV",
        js: "JS", ts: "TS", html: "HTML", css: "CSS", py: "PY",
    };
    return (map[ext] ?? ext.toUpperCase().slice(0, 4)) || "FILE";
}

// ── Upload state ──────────────────────────────────────
const uploadStats = {
    totalBytes: 0,
    startTime: null,
    lastLoaded: 0,
    lastTime: null,
};

// fileStates: array of { name, size, status: 'pending'|'uploading'|'done'|'error', progress: 0–100, error: string|null }
let fileStates = [];

// ── File list rendering ───────────────────────────────
function renderFileList() {
    const list = document.getElementById("upload-toast-file-list");
    if (!list) return;

    list.innerHTML = fileStates.map((f, i) => {
        const typeTag = `<span class="upload-file-type">${fileTypeLabel(f.name)}</span>`;
        const nameEl = `<span class="upload-file-name">${escapeHtml(f.name)}</span>`;
        const sizeEl = `<span class="upload-file-size">${formatBytes(f.size)}</span>`;

        let statusEl;
        if (f.status === "done") {
            statusEl = `<span class="upload-file-status upload-file-status--done">done</span>`;
        } else if (f.status === "error") {
            statusEl = `<span class="upload-file-status upload-file-status--error">failed</span>`;
        } else if (f.status === "uploading") {
            statusEl = `<span class="upload-file-status upload-file-status--uploading">uploading</span>`;
        } else {
            statusEl = `<span class="upload-file-status upload-file-status--pending">waiting</span>`;
        }

        const miniBar = (f.status === "uploading")
            ? `<div class="upload-file-minibar"><div class="upload-file-minibar-fill" style="width:${f.progress}%"></div></div>`
            : "";

        const errorNote = f.error
            ? `<span class="upload-file-error">${escapeHtml(f.error)}</span>`
            : "";

        return `
        <div class="upload-file-row" data-index="${i}">
            <div class="upload-file-meta">
                ${typeTag}
                <div class="upload-file-info">
                    ${nameEl}
                    ${miniBar}
                    ${errorNote}
                </div>
            </div>
            <div class="upload-file-right">
                ${sizeEl}
                ${statusEl}
            </div>
        </div>`;
    }).join("");
}

function escapeHtml(str) {
    return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// ── Progress updates ──────────────────────────────────

// Called continuously during XHR progress.
// We approximate per-file progress by tracking total bytes loaded.
function setUploadProgress(percent, loaded, total) {
    const bc = document.getElementById("breadcrumb");
    const fill = document.getElementById("breadcrumb-fill");
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastFill = document.getElementById("upload-toast-fill");
    const toastPct = document.getElementById("upload-toast-pct");
    const toastSpeed = document.getElementById("upload-toast-speed");
    const toastEta = document.getElementById("upload-toast-eta");

    if (!fill || !badge) return;

    bc?.classList.add("uploading");
    badge.classList.add("visible");

    fill.style.width = percent + "%";
    if (badgeText) badgeText.textContent = percent + "%";
    if (toastFill) toastFill.style.width = percent + "%";
    if (toastPct) toastPct.textContent = percent + "%";

    if (loaded !== undefined && total !== undefined) {
        const now = Date.now();
        if (uploadStats.lastTime !== null) {
            const dt = (now - uploadStats.lastTime) / 1000;
            const dl = loaded - uploadStats.lastLoaded;
            if (dt > 0) {
                const speed = dl / dt;
                if (toastSpeed) toastSpeed.textContent = formatSpeed(speed);
                const eta = speed > 0 ? (total - loaded) / speed : Infinity;
                if (toastEta) toastEta.textContent = formatEta(eta) ?? "";
            }
        }
        uploadStats.lastLoaded = loaded;
        uploadStats.lastTime = now;

        // Approximate which file is currently uploading based on cumulative byte offset.
        let cursor = 0;
        for (let i = 0; i < fileStates.length; i++) {
            const f = fileStates[i];
            const fileEnd = cursor + f.size;
            if (loaded <= cursor) {
                // Not started yet
                if (f.status === "uploading") { f.status = "pending"; f.progress = 0; }
            } else if (loaded >= fileEnd) {
                // Fully uploaded
                if (f.status !== "done" && f.status !== "error") {
                    f.status = "done";
                    f.progress = 100;
                }
            } else {
                // Currently uploading this file
                f.status = "uploading";
                f.progress = Math.round(((loaded - cursor) / f.size) * 100);
            }
            cursor = fileEnd;
        }
        renderFileList();
    }
}

function finishUploadProgress(succeeded, failed, totalBytes, durationMs) {
    const bc = document.getElementById("breadcrumb");
    const fill = document.getElementById("breadcrumb-fill");
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastTitle = document.querySelector(".upload-toast-title");
    const toastFill = document.getElementById("upload-toast-fill");
    const toastPct = document.getElementById("upload-toast-pct");
    const toastSub = document.getElementById("upload-toast-sub");
    const toastSpeed = document.getElementById("upload-toast-speed");
    const toastEta = document.getElementById("upload-toast-eta");
    const dot = document.querySelector(".upload-badge-dot");

    const partialFail = failed > 0;
    const color = partialFail ? "#ff9800" : "#4caf50";

    if (fill) {
        fill.style.width = "100%";
        fill.style.setProperty('--_breadcrumb-fill_color', color);
    }
    if (toastFill) { toastFill.style.background = color; toastFill.style.width = "100%"; }
    if (toastPct) toastPct.textContent = "100%";
    if (toastTitle) toastTitle.textContent = partialFail ? `${succeeded} uploaded, ${failed} failed` : "Upload complete";
    if (badgeText) badgeText.textContent = partialFail ? `${succeeded}/${succeeded + failed}` : "Done";
    if (dot) { dot.style.animation = "none"; dot.style.background = color; }
    if (bc) bc.style.borderColor = `color-mix(in srgb, ${color} 45%, transparent)`;

    if (toastSub && totalBytes && durationMs) {
        const avgSpeed = totalBytes / (durationMs / 1000);
        toastSub.textContent = `${formatBytes(totalBytes)} · avg ${formatSpeed(avgSpeed)}`;
    }
    if (toastSpeed) toastSpeed.textContent = "";
    if (toastEta) toastEta.textContent = durationMs ? `Completed in ${(durationMs / 1000).toFixed(1)}s` : "";

    // Mark all remaining non-errored files as done
    fileStates.forEach(f => { if (f.status !== "error") { f.status = "done"; f.progress = 100; } });
    renderFileList();

    localStorage.setItem("uploadResult", JSON.stringify({
        succeeded, failed, color, toastWasOpen: uploadToastOpen, totalBytes, durationMs,
        fileStates
    }));
    setTimeout(() => location.reload(), 2500);
}

function handleUploadError(reason) {
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastTitle = document.querySelector(".upload-toast-title");
    const toastFill = document.getElementById("upload-toast-fill");
    const toastSpeed = document.getElementById("upload-toast-speed");
    const toastEta = document.getElementById("upload-toast-eta");
    const dot = document.querySelector(".upload-badge-dot");

    if (badgeText) badgeText.textContent = "Error";
    if (badge) badge.style.color = "#f44336";
    if (toastTitle) toastTitle.textContent = `Upload failed: ${reason}`;
    if (toastFill) toastFill.style.background = "#f44336";
    if (toastSpeed) toastSpeed.textContent = "";
    if (toastEta) toastEta.textContent = "";
    if (dot) { dot.style.animation = "none"; dot.style.background = "#f44336"; }

    fileStates.forEach(f => { if (f.status === "uploading" || f.status === "pending") { f.status = "error"; f.error = reason; } });
    renderFileList();

    uploadToastOpen = true;
    document.getElementById("upload-toast")?.classList.add("visible");
    document.getElementById("uploadButtonText").textContent = "Upload";
}

// ── Mark individual file as failed (from server results) ──
function markFileFailed(fileName, reason) {
    const f = fileStates.find(s => s.name === fileName);
    if (f) { f.status = "error"; f.error = reason ?? "Upload failed"; }
}

// ── Init ─────────────────────────────────────────────
document.addEventListener("DOMContentLoaded", () => {
    const header = document.querySelector("header");
    if (header) {
        const container = document.querySelector(".container");
        if (container) container.style.marginTop = `${header.offsetHeight + 12}px`;
    }
    new LazyLoad({
        elements_selector: ".media-container img[data-src]",
        callback_enter: () => !isLightboxOpen,
        load_delay: 300,
    });
    document.querySelectorAll(".media-container").forEach(setupMediaContainer);
    document.getElementById("lightbox-close")?.addEventListener("click", closeLightbox);
    document.getElementById("lightbox")?.addEventListener("click", e => {
        if (e.target === e.currentTarget) closeLightbox();
    });
    document.addEventListener("keydown", e => {
        if (!isLightboxOpen) return;
        if (e.key === "Escape") closeLightbox();
        if (e.key === "ArrowRight") navigateLightbox(1);
        if (e.key === "ArrowLeft") navigateLightbox(-1);
    });

    // ── Restore toast state after reload ─────────────
    const saved = localStorage.getItem("uploadResult");
    if (saved) {
        localStorage.removeItem("uploadResult");
        const { succeeded, failed, color, toastWasOpen, totalBytes, durationMs, fileStates: savedFiles } = JSON.parse(saved);

        const badge = document.getElementById("upload-badge");
        const badgeText = document.getElementById("upload-badge-text");
        const toastFill = document.getElementById("upload-toast-fill");
        const toastPct = document.getElementById("upload-toast-pct");
        const toastTitle = document.querySelector(".upload-toast-title");
        const toastSub = document.getElementById("upload-toast-sub");
        const toastEta = document.getElementById("upload-toast-eta");
        const dot = document.querySelector(".upload-badge-dot");

        if (savedFiles) {
            fileStates = savedFiles;
            renderFileList();
        }
        if (toastFill) { toastFill.style.background = color; toastFill.style.width = "100%"; }
        if (toastPct) toastPct.textContent = "100%";
        if (toastTitle) toastTitle.textContent = failed > 0 ? `${succeeded} uploaded, ${failed} failed` : "Upload complete";
        if (badgeText) badgeText.textContent = failed > 0 ? `${succeeded}/${succeeded + failed}` : "Done";
        if (dot) { dot.style.animation = "none"; dot.style.background = color; }

        if (toastSub && totalBytes && durationMs) {
            const avgSpeed = totalBytes / (durationMs / 1000);
            toastSub.textContent = `${formatBytes(totalBytes)} · avg ${formatSpeed(avgSpeed)}`;
        }
        if (toastEta && durationMs) {
            toastEta.textContent = `Completed in ${(durationMs / 1000).toFixed(1)}s`;
        }

        badge?.classList.add("visible");
        uploadToastOpen = toastWasOpen ?? false;
        if (uploadToastOpen) {
            document.getElementById("upload-toast")?.classList.add("visible");
            setTimeout(() => {
                uploadToastOpen = false;
                document.getElementById("upload-toast")?.classList.remove("visible");
            }, 4000);
        }
    }

    // ── Upload ───────────────────────────────────────
    const uploadForm = document.getElementById("uploadForm");
    if (!uploadForm) return;

    window.submitUpload = async function () {
        const input = document.getElementById("fileUpload");
        const formData = new FormData();
        let totalBytes = 0;

        // Build fileStates from selected files
        fileStates = [];
        for (let i = 0; i < input.files.length; i++) {
            const file = input.files[i];
            formData.append("files", file);
            totalBytes += file.size;
            fileStates.push({ name: file.name, size: file.size, status: "pending", progress: 0, error: null });
        }
        const fileCount = fileStates.length;

        // Mark first file as uploading immediately
        if (fileStates.length > 0) fileStates[0].status = "uploading";
        renderFileList();

        // Reset stats
        uploadStats.totalBytes = totalBytes;
        uploadStats.startTime = Date.now();
        uploadStats.lastLoaded = 0;
        uploadStats.lastTime = null;

        const toastTitle = document.querySelector(".upload-toast-title");
        if (toastTitle) toastTitle.textContent = `Uploading ${fileCount} file${fileCount !== 1 ? "s" : ""}…`;

        setUploadProgress(0, 0, totalBytes);
        document.getElementById("uploadButtonText").textContent = "Uploading…";

        // Open the toast automatically so the user sees what's happening
        if (!uploadToastOpen) {
            uploadToastOpen = true;
            document.getElementById("upload-toast")?.classList.add("visible");
        }

        await fetch("/api/log", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ message: "Upload initiated" })
        });

        const xhr = new XMLHttpRequest();

        xhr.upload.addEventListener("progress", e => {
            if (e.lengthComputable) {
                const percent = Math.round((e.loaded / e.total) * 100);
                setUploadProgress(percent, e.loaded, e.total);
            }
        });

        xhr.onload = () => {
            if (xhr.status !== 200) {
                handleUploadError(`HTTP ${xhr.status}`);
                return;
            }
            try {
                const { results } = JSON.parse(xhr.responseText);
                const failed = results.filter(r => !r.ok);
                const succeeded = results.filter(r => r.ok);
                const durationMs = Date.now() - uploadStats.startTime;

                // Propagate per-file errors from server response
                failed.forEach(r => markFileFailed(r.name, r.error));

                if (failed.length === 0) {
                    finishUploadProgress(succeeded.length, 0, totalBytes, durationMs);
                } else if (succeeded.length === 0) {
                    handleUploadError(`All ${failed.length} files failed`);
                } else {
                    finishUploadProgress(succeeded.length, failed.length, totalBytes, durationMs);
                    console.warn("Failed uploads:", failed);
                }
            } catch {
                handleUploadError("Invalid server response");
            }
        };

        xhr.onerror = () => handleUploadError("Network error");

        xhr.open("POST", window.location.href);
        xhr.send(formData);
    };
});