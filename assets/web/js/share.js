const DISPLAY = { FLEX: "flex", BLOCK: "block", NONE: "none" };

// ── Dark mode ────────────────────────────────────────
// (class already set in <head> to prevent flash)
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

// ── Upload Progress ───────────────────────────────────
function setUploadProgress(percent) {
    const bc = document.getElementById("breadcrumb");
    const fill = document.getElementById("breadcrumb-fill");
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastFill = document.getElementById("upload-toast-fill");
    const toastPct = document.getElementById("upload-toast-pct");

    if (!fill || !badge) return;

    bc?.classList.add("uploading");
    badge.classList.add("visible");

    fill.style.width = percent + "%";
    if (badgeText) badgeText.textContent = percent + "%";
    if (toastFill) toastFill.style.width = percent + "%";
    if (toastPct) toastPct.textContent = percent + "%";
}

function finishUploadProgress(succeeded, failed) {
    const bc = document.getElementById("breadcrumb");
    const fill = document.getElementById("breadcrumb-fill");
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastTitle = document.querySelector(".upload-toast-title");
    const toastFill = document.getElementById("upload-toast-fill");
    const dot = document.querySelector(".upload-badge-dot");

    const partialFail = failed > 0;
    const color = partialFail ? "#ff9800" : "#4caf50";

    if (fill) {
        fill.style.width = "100%";
        fill.style.background = `color-mix(in srgb, ${color} 12%, transparent)`;
    }
    if (toastFill) {
        toastFill.style.background = color;
        toastFill.style.width = "100%";
    }
    if (toastTitle && !partialFail) toastTitle.textContent = "Upload complete ✓";
    if (badgeText) badgeText.textContent = partialFail ? `⚠️ ${succeeded}/${succeeded + failed}` : "✓ Done";
    if (dot) { dot.style.animation = "none"; dot.style.background = color; }
    if (bc) bc.style.borderColor = `color-mix(in srgb, ${color} 45%, transparent)`;

    setTimeout(() => location.reload(), 2500);
}

function handleUploadError(reason) {
    const badge = document.getElementById("upload-badge");
    const badgeText = document.getElementById("upload-badge-text");
    const toastTitle = document.querySelector(".upload-toast-title");
    const toastFill = document.getElementById("upload-toast-fill");
    const dot = document.querySelector(".upload-badge-dot");

    if (badgeText) badgeText.textContent = "✗ Error";
    if (badge) badge.style.color = "#f44336";
    if (toastTitle) toastTitle.textContent = `Upload failed: ${reason}`;
    if (toastFill) toastFill.style.background = "#f44336";
    if (dot) { dot.style.animation = "none"; dot.style.background = "#f44336"; }

    // Toast öffnen damit der User den Fehler sieht
    uploadToastOpen = true;
    document.getElementById("upload-toast")?.classList.add("visible");

    document.getElementById("uploadButtonText").textContent = "📤 Upload";
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

    // ── Upload ───────────────────────────────────────
    const uploadForm = document.getElementById("uploadForm");
    if (!uploadForm) return;

    window.submitUpload = async function () {
        const input = document.getElementById("fileUpload");
        const formData = new FormData();
        const fileCount = input.files.length;
        for (let i = 0; i < fileCount; i++) {
            formData.append("files", input.files[i]);
        }

        const toastSub = document.getElementById("upload-toast-sub");
        if (toastSub) toastSub.textContent = `${fileCount} file${fileCount !== 1 ? "s" : ""}`;

        setUploadProgress(0);
        document.getElementById("uploadButtonText").textContent = "⏳ Loading…";

        await fetch("/api/log", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ message: "Upload initiated" })
        });

        const xhr = new XMLHttpRequest();

        xhr.upload.addEventListener("progress", e => {
            if (e.lengthComputable) {
                setUploadProgress(Math.round((e.loaded / e.total) * 100));
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

                if (failed.length === 0) {
                    finishUploadProgress(succeeded.length, 0);
                } else if (succeeded.length === 0) {
                    handleUploadError(`All ${failed.length} files failed`);
                } else {
                    finishUploadProgress(succeeded.length, failed.length);
                    const toastTitle = document.querySelector(".upload-toast-title");
                    if (toastTitle) toastTitle.textContent = `${succeeded.length} ok, ${failed.length} failed ⚠️`;
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