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

    window.submitUpload = function () {
        const input = document.getElementById("fileUpload");
        const formData = new FormData();
        for (let i = 0; i < input.files.length; i++) {
            formData.append("files", input.files[i]);
        }
        fetch(window.location.href, { method: "POST", body: formData })
            .then(res => res.text())
            .then(() => {
                const btn = document.getElementById("uploadButtonText");
                btn.textContent = "✓ Uploaded!";
                setTimeout(() => location.reload(), 3000);
            })
            .catch(err => alert("Upload error: " + err));
    };
});