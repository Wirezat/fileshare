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
    if (d < new Date()) return '<span class="pill pill-expired">inactive</span>';
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

// ── Inline editing ────────────────────────────────────

/**
 * Send a PATCH to update one or more fields of a share.
 * Expects the server to accept: PATCH /admin/api/shares?subpath=sub
 * with a JSON body of the changed fields merged with the full share object.
 */
async function updateShare(sub, patch) {
    try {
        const res = await fetch(API + '?subpath=' + encodeURIComponent(sub), {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patch)
        });
        if (!res.ok) throw new Error(await res.text() || res.statusText);
        showStatus('status-shares', '/' + sub + ' updated', 'ok');
        loadShares();
    } catch (err) {
        showStatus('status-shares', err.message, 'err');
        loadShares(); // re-render to revert optimistic UI
    }
}

/**
 * Replace a display cell with an <input>, save on blur or Enter,
 * cancel on Escape and restore the original display value.
 */
function makeEditable(td, currentValue, type, onSave) {
    // Don't open two editors at once
    if (td.querySelector('input')) return;

    const displayHTML = td.innerHTML;

    const input = document.createElement('input');
    input.type = type;
    input.value = currentValue;
    input.style.cssText = `
        width: 100%;
        box-sizing: border-box;
        background: var(--bg-input, var(--bg-card));
        border: 1px solid var(--accent, #4f8ef7);
        border-radius: 4px;
        color: var(--text);
        font-size: 13px;
        padding: 2px 6px;
        outline: none;
    `;

    td.innerHTML = '';
    td.appendChild(input);
    input.focus();
    input.select();

    function save() {
        const val = type === 'number' ? parseInt(input.value, 10) : input.value;
        if (!isNaN(val) || type !== 'number') {
            onSave(val);
        } else {
            td.innerHTML = displayHTML; // restore on bad input
        }
    }

    function cancel() {
        td.innerHTML = displayHTML;
    }

    input.addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); save(); }
        if (e.key === 'Escape') { e.preventDefault(); cancel(); }
    });
    input.addEventListener('blur', save);
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

        tbody.innerHTML = '';

        keys.sort().forEach(sub => {
            const s = shares[sub];
            const isExp = s.expired;
            const tr = document.createElement('tr');

            // ── Subpath (read-only link)
            const tdSub = document.createElement('td');
            tdSub.innerHTML = `<a class="subpath" href="/${sub}" target="_blank">/${sub}</a>`;

            // ── Path (click to edit) ──────────────────
            const tdPath = document.createElement('td');
            tdPath.className = 'editable-cell';
            tdPath.title = 'Click to edit';
            tdPath.innerHTML = `<span class="path-text" title="${s.path}">${s.path}</span>`;
            tdPath.addEventListener('click', () => {
                makeEditable(tdPath, s.path, 'text', val => {
                    if (val.trim()) updateShare(sub, { path: val.trim() });
                    else tdPath.innerHTML = `<span class="path-text" title="${s.path}">${s.path}</span>`;
                });
            });

            // ── Uses (click to edit) ──────────────────
            const tdUses = document.createElement('td');
            tdUses.className = 'hide-sm editable-cell';
            tdUses.title = 'Click to edit';
            tdUses.innerHTML = fmtUses(s.uses);
            tdUses.addEventListener('click', () => {
                makeEditable(tdUses, s.uses, 'number', val => {
                    updateShare(sub, { uses: val });
                });
            });

            // ── Expiration (click to edit) ────────────
            const tdExp = document.createElement('td');
            tdExp.className = 'hide-sm editable-cell';
            tdExp.title = 'Click to edit (unix timestamp, 0 = never)';
            tdExp.innerHTML = fmtExp(s.expiration);
            tdExp.addEventListener('click', () => {
                makeEditable(tdExp, s.expiration, 'number', val => {
                    updateShare(sub, { expiration: val });
                });
            });

            // ── Upload toggle ─────────────────────────
            const tdUpload = document.createElement('td');
            tdUpload.className = 'editable-cell';
            tdUpload.title = 'Click to toggle';
            tdUpload.innerHTML = s.allow_post
                ? '<span class="pill pill-yes">on</span>'
                : '<span class="pill pill-no">off</span>';
            tdUpload.style.cursor = 'pointer';
            tdUpload.addEventListener('click', () => {
                // Optimistic UI flip
                const next = !s.allow_post;
                tdUpload.innerHTML = next
                    ? '<span class="pill pill-yes">on</span>'
                    : '<span class="pill pill-no">off</span>';
                updateShare(sub, { allow_post: next });
            });

            // ── Status toggle (active ↔ expired) ────
            //TODO: new tag: "inactive"
            const tdStatus = document.createElement('td');
            tdStatus.className = 'editable-cell';
            tdStatus.title = 'Click to toggle';
            tdStatus.style.cursor = 'pointer';
            tdStatus.innerHTML = isExp
                ? '<span class="pill pill-expired">expired</span>'
                : '<span class="pill pill-active">active</span>';
            tdStatus.addEventListener('click', () => {
                const next = !isExp;
                tdStatus.innerHTML = next
                    ? '<span class="pill pill-expired">expired</span>'
                    : '<span class="pill pill-active">active</span>';
                updateShare(sub, { expired: next });
            });

            // ── Delete
            const tdDel = document.createElement('td');
            tdDel.innerHTML = `<button class="btn btn-danger-ghost" onclick="deleteShare('${sub}')">Delete</button>`;

            tr.append(tdSub, tdPath, tdUses, tdExp, tdUpload, tdStatus, tdDel);
            tbody.appendChild(tr);
        });

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

    const time = entry.time.substring(11, 19);

    let parsed = null;
    try { parsed = JSON.parse(entry.message); } catch (_) { }

    if (parsed?.method && parsed?.url) {
        div.className = 'log-line-request';
        div.style.cursor = 'pointer';

        const detail = document.createElement('pre');
        detail.textContent = JSON.stringify(parsed, null, 2);
        detail.className = 'log-detail';

        div.textContent = `[${entry.level}] ${time} — ${parsed.client_ip ?? ''}: ${parsed.method} ${parsed.url}`;
        div.appendChild(detail);
        div.onclick = () => {
            const open = detail.style.display === 'block';
            detail.style.display = open ? 'none' : 'block';
        };
    } else {
        div.textContent = `[${entry.level}] ${time} — ${entry.message}`;
    }

    box.appendChild(div);
    box.scrollTop = box.scrollHeight;
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
        console.warn('SSE connection lost, retrying...');
    };
}

function applyFilter() {
    const active = new Set(
        [...document.querySelectorAll('.log-filter:checked')].map(el => el.value)
    );
    document.querySelectorAll('#log-box > div').forEach(div => {
        div.style.display = active.has(div.dataset.level) ? '' : 'none';
    });
}

loadShares();
loadLogs();