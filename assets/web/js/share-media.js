// ── Media menu ────────────────────────────────────────
const MEDIA_ICON = {
    play: "M8 5v14l11-7z",
    pause: "M7 5h4v14H7zM13 5h4v14h-4z",
    volume: "M4 9v6h4l5 4V5L8 9zM16 8.5a4 4 0 010 7",
    fullscreen: "M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5",
};
const SEEK_STEP = 5;

/* Pauses every audio and video on the page except except (null pauses all). */
function pauseOtherMedia(except) {
    document.querySelectorAll("audio, video").forEach(m => { if (m !== except && !m.paused) m.pause(); });
}

/* Formats s seconds as m:ss or h:mm:ss; unknown durations give 0:00.
   @returns {string} */
function formatTime(s) {
    if (!isFinite(s) || s < 0) s = 0;
    const total = Math.floor(s), h = Math.floor(total / 3600), m = Math.floor(total % 3600 / 60), sec = total % 60;
    const pad = n => String(n).padStart(2, "0");
    return h ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`;
}

/* Builds an SVG button labelled with the share text labelKey.
   @returns {HTMLButtonElement} */
function mediaButton(icon, labelKey) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "media-btn media-btn-" + icon;
    setMediaIcon(btn, icon, labelKey);
    return btn;
}

/* Shows icon on btn and labels it with the share text labelKey. */
function setMediaIcon(btn, icon, labelKey) {
    btn.replaceChildren(svgIcon(MEDIA_ICON[icon]));
    btn.setAttribute("aria-label", t(labelKey));
}

/* Builds the progress slider of media: click or drag to seek, arrow keys move by SEEK_STEP seconds.
   @returns {HTMLDivElement} */
function createSeek(media) {
    const seek = document.createElement("div");
    seek.className = "media-seek";
    seek.tabIndex = 0;
    seek.setAttribute("role", "slider");
    seek.setAttribute("aria-label", t("share.media.seek"));
    seek.setAttribute("aria-valuemin", "0");
    const fill = document.createElement("div");
    fill.className = "media-seek-fill";
    seek.appendChild(fill);

    const update = () => {
        const d = isFinite(media.duration) ? media.duration : 0;
        fill.style.width = d ? `${media.currentTime / d * 100}%` : "0";
        seek.setAttribute("aria-valuemax", String(Math.floor(d)));
        seek.setAttribute("aria-valuenow", String(Math.floor(media.currentTime)));
        seek.setAttribute("aria-valuetext", formatTime(media.currentTime));
    };
    const seekTo = x => {
        const r = seek.getBoundingClientRect();
        if (!r.width || !isFinite(media.duration)) return;
        media.currentTime = Math.min(Math.max((x - r.left) / r.width, 0), 1) * media.duration;
    };
    let dragging = false;
    seek.addEventListener("pointerdown", e => {
        dragging = true;
        seek.setPointerCapture(e.pointerId);
        seekTo(e.clientX);
    });
    seek.addEventListener("pointermove", e => { if (dragging) seekTo(e.clientX); });
    seek.addEventListener("pointerup", () => { dragging = false; });
    seek.addEventListener("pointercancel", () => { dragging = false; });
    seek.addEventListener("keydown", e => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        e.preventDefault();
        e.stopPropagation();
        const end = isFinite(media.duration) ? media.duration : 0;
        media.currentTime = Math.min(Math.max(media.currentTime + (e.key === "ArrowRight" ? SEEK_STEP : -SEEK_STEP), 0), end);
    });
    ["timeupdate", "durationchange", "loadedmetadata"].forEach(ev => media.addEventListener(ev, update));
    update();
    return seek;
}

/* Builds the play menu for media: on a card a capsule with play and duration that turns into a bar after
   the first play, in the lightbox a wide capsule with time, seek, volume and full screen of fullscreenTarget.
   @returns {HTMLDivElement} */
function createMediaMenu(media, variant, fullscreenTarget) {
    const menu = document.createElement("div");
    menu.className = `media-menu media-menu-${variant}`;
    const play = mediaButton("play", "share.media.play");
    const seek = createSeek(media);
    const cur = Object.assign(document.createElement("span"), { className: "media-time media-time-current" });
    const total = Object.assign(document.createElement("span"), { className: "media-time media-time-total" });
    const toggle = () => { media.paused ? media.play().catch(() => {}) : media.pause(); };

    play.addEventListener("click", toggle);
    menu.addEventListener("click", e => e.preventDefault());
    menu.addEventListener("keydown", e => {
        if (e.key === " " && !e.target.closest("input")) { e.preventDefault(); toggle(); }
    });
    media.addEventListener("play", () => {
        pauseOtherMedia(media);
        setMediaIcon(play, "pause", "share.media.pause");
        menu.classList.add("started");
    });
    media.addEventListener("pause", () => setMediaIcon(play, "play", "share.media.play"));
    media.addEventListener("timeupdate", () => { cur.textContent = formatTime(media.currentTime); });
    const showTotal = () => {
        if (isFinite(media.duration)) { total.textContent = formatTime(media.duration); menu.classList.add("has-duration"); }
    };
    media.addEventListener("loadedmetadata", showTotal);
    media.addEventListener("durationchange", showTotal);
    cur.textContent = formatTime(0);
    showTotal();

    if (variant === "card") {
        const time = Object.assign(document.createElement("span"), { className: "media-times" });
        const sep = Object.assign(document.createElement("span"), { className: "media-time-sep", textContent: "/" });
        time.append(cur, sep, total);
        menu.append(play, seek, time);
        return menu;
    }

    const volume = document.createElement("input");
    volume.type = "range";
    volume.className = "media-volume";
    volume.min = "0";
    volume.max = "1";
    volume.step = "0.05";
    volume.value = String(media.volume);
    volume.setAttribute("aria-label", t("share.media.volume"));
    volume.addEventListener("input", () => {
        media.volume = Number(volume.value);
        media.muted = media.volume === 0;
    });
    const full = mediaButton("fullscreen", "share.media.fullscreen");
    full.addEventListener("click", () => {
        const el = fullscreenTarget ?? media;
        try { el.requestFullscreen?.()?.catch?.(() => {}); } catch { }
    });
    menu.append(play, cur, seek, total, volume, full);
    return menu;
}
