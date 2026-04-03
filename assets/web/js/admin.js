const API = '/admin/api/shares';

// ── Dark mode ──────────────────────────────────────────
function initTheme() {
    updateThemeBtn();
}
function updateThemeBtn() {
    const btn = document.getElementById('theme-toggle');
    if (btn) btn.textContent = document.documentElement.classList.contains('dark') ? '☀️' : '🌙';
}
function toggleTheme() {
    const isDark = document.documentElement.classList.toggle('dark');
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
    updateThemeBtn();
}
initTheme();

// ── Tabs ──────────────────────────────────────────────
function switchTab(name) {
    document.querySelectorAll('.tab-panel').forEach(p => p.classList.remove('active'));
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.getElementById('tab-' + name).classList.add('active');
    event.currentTarget.classList.add('active');
}

// ── Helpers ───────────────────────────────────────────
function fmtExp(ts) {
    if (!ts || ts === 0) return '<span style="color:var(--text-faint);font-size:12px;">never</span>';
    const d = new Date(ts * 1000);
    if (d < new Date()) return '<span class="pill pill-expired">expired</span>';
    const diff = d - Date.now();
    const days = Math.floor(diff / 86400000);
    const hours = Math.floor((diff % 86400000) / 3600000);
    return `<span style="font-size:12px;color:var(--text-muted);" title="${d.toLocaleString()}">${days}d ${hours}h</span>`;
}
function fmtUses(u) {
    if (u === -1) return '<span style="font-size:15px;">∞</span>';
    if (u === 0) return '<span class="pill pill-expired">0</span>';
    return `<span style="font-size:13px;">${u}</span>`;
}
function showStatus(id, msg, type) {
    const el = document.getElementById(id);
    if (!el) return;
    el.textContent = msg;
    el.className = 'status-msg ' + type;
    setTimeout(() => { el.className = 'status-msg'; }, 3500);
}

// ── Shares ────────────────────────────────────────────
function updateStats(shares) {
    const keys = Object.keys(shares);
    const now = Date.now() / 1000;
    const expired = keys.filter(k => {
        const s = shares[k];
        return s.expired || (s.expiration !== 0 && s.expiration < now) || s.uses === 0;
    });
    const withUpload = keys.filter(k => shares[k].allow_post);
    document.getElementById('stat-total').textContent = keys.length;
    document.getElementById('stat-active').textContent = keys.length - expired.length;
    document.getElementById('stat-expired').textContent = expired.length;
    document.getElementById('stat-upload').textContent = withUpload.length;
}

async function loadShares() {
    const tbody = document.getElementById('shares-body');
    try {
        const res = await fetch(API);
        if (!res.ok) throw new Error(res.statusText);
        const shares = await res.json();
        const keys = Object.keys(shares);
        updateStats(shares);
        if (keys.length === 0) {
            tbody.innerHTML = `<tr><td colspan="7" class="table-info"><span class="table-info-icon">📭</span>No shares yet. Add one above.</td></tr>`;
            return;
        }
        tbody.innerHTML = keys.sort().map(sub => {
            const s = shares[sub];
            const isExp = s.expired;
            return `<tr>
        <td><a class="subpath" href="/${sub}" target="_blank">/${sub}</a></td>
        <td><span class="path-text" title="${s.path}">${s.path}</span></td>
        <td class="hide-sm">${fmtUses(s.uses)}</td>
        <td class="hide-sm">${fmtExp(s.expiration)}</td>
        <td>${s.allow_post ? '<span class="pill pill-yes">on</span>' : '<span class="pill pill-no">off</span>'}</td>
        <td>${isExp ? '<span class="pill pill-expired">expired</span>' : '<span class="pill pill-active">active</span>'}</td>
        <td><button class="btn btn-danger-ghost" onclick="deleteShare('${sub}')">Delete</button></td>
        </tr>`;
        }).join('');
    } catch (err) {
        tbody.innerHTML = `<tr><td colspan="7" class="table-info" style="color:var(--danger);"><span class="table-info-icon">⚠</span>Failed to load: ${err.message}</td></tr>`;
    }
}

async function addShare() {
    let subpath = document.getElementById('f-subpath').value.trim();
    const path = document.getElementById('f-path').value.trim();
    const uses = parseInt(document.getElementById('f-uses').value);
    const expiration = parseInt(document.getElementById('f-expiration').value);
    const allowPost = document.getElementById('f-allowpost').checked;
    if (!path) { showStatus('status-shares', 'Path is required', 'err'); return; }
    if (!subpath) {
        const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
        subpath = Array.from({ length: 12 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
    }
    try {
        const res = await fetch(API, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ subpath, path, uses, expiration, allow_post: allowPost })
        });
        if (!res.ok) throw new Error(await res.text() || res.statusText);
        showStatus('status-shares', '/' + subpath + ' added', 'ok');
        resetForm();
        loadShares();
    } catch (err) { showStatus('status-shares', err.message, 'err'); }
}

async function deleteShare(sub) {
    if (!confirm('Delete /' + sub + '?')) return;
    try {
        const res = await fetch(API + '?subpath=' + encodeURIComponent(sub), { method: 'DELETE' });
        if (!res.ok) throw new Error(await res.text());
        showStatus('status-shares', '/' + sub + ' deleted', 'ok');
        loadShares();
    } catch (err) { showStatus('status-shares', err.message, 'err'); }
}

function resetForm() {
    document.getElementById('f-subpath').value = '';
    document.getElementById('f-path').value = '';
    document.getElementById('f-uses').value = '-1';
    document.getElementById('f-expiration').value = '0';
    document.getElementById('f-allowpost').checked = false;
}

// ── Settings ──────────────────────────────────────────
async function changePassword() {
    const current = document.getElementById('s-pw-current').value;
    const pw1 = document.getElementById('s-pw-new').value;
    const pw2 = document.getElementById('s-pw-confirm').value;
    if (!current) { showStatus('status-pw', 'Current password is required', 'err'); return; }
    if (!pw1) { showStatus('status-pw', 'New password cannot be empty', 'err'); return; }
    if (pw1 !== pw2) { showStatus('status-pw', 'Passwords do not match', 'err'); return; }
    try {
        const res = await fetch('/admin/api/settings/password', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ current_password: current, new_password: pw1 })
        });
        if (!res.ok) throw new Error(await res.text());
        showStatus('status-pw', 'Password updated!', 'ok');
        document.getElementById('s-pw-current').value = '';
        document.getElementById('s-pw-new').value = '';
        document.getElementById('s-pw-confirm').value = '';
    } catch (err) { showStatus('status-pw', err.message, 'err'); }
}

async function pruneExpired() {
    if (!confirm('Delete all expired shares from data.json?')) return;
    try {
        const res = await fetch('/admin/api/settings/prune_expired', { method: 'POST' });
        if (!res.ok) throw new Error(await res.text());
        showStatus('status-prune', 'Expired shares deleted', 'ok');
        loadShares();
    } catch (err) { showStatus('status-prune', err.message, 'err'); }
}

let logEventSource = null;

function clearLog() {
    document.getElementById('log-box').innerHTML = '';
}

const LOG_CLASSES = {
    INFO: 'log-line-info',
    WARN: 'log-line-warn',
    ERROR: 'log-line-error',
    REQUEST: 'log-line-request',
};

function appendLogLine(entry) {
    const box = document.getElementById('log-box');
    const div = document.createElement('div');
    div.className = LOG_CLASSES[entry.level] ?? '';
    div.dataset.level = entry.level;
    div.textContent = `[${entry.level}] ${entry.time} — ${entry.message}`;
    box.appendChild(div);
    box.scrollTop = box.scrollHeight;
}

function applyFilter() {
    const active = new Set(
        [...document.querySelectorAll('.log-filter:checked')].map(el => el.value)
    );
    document.querySelectorAll('#log-box > div[data-level]').forEach(el => {
        el.hidden = !active.has(el.dataset.level);
    });
}

async function loadLogs() {
    try {
        const res = await fetch('/admin/api/logs?n=200');
        if (!res.ok) throw new Error(res.statusText);
        const entries = await res.json();
        entries.forEach(appendLogLine);
    } catch (err) {
        const box = document.getElementById('log-box');
        box.innerHTML = `<span style="color:var(--danger);">Failed to load logs: ${err.message}</span>`;
    }

    if (logEventSource) logEventSource.close();
    logEventSource = new EventSource('/admin/api/logs/stream');
    logEventSource.onmessage = e => {
        const entry = JSON.parse(e.data);
        appendLogLine(entry);
        applyFilter();
    };
    logEventSource.onerror = () => {
    };
}

loadShares();
loadLogs();