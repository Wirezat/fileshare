const API = '/admin/api/shares';

// ── Dark mode ──────────────────────────────────────────
function updateThemeBtn() {
    const btn = document.getElementById('theme-toggle');
    if (btn) btn.textContent = document.documentElement.classList.contains('dark') ? '☀️' : '🌙';
}
function toggleTheme() {
    const isDark = document.documentElement.classList.toggle('dark');
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
    updateThemeBtn();
}
updateThemeBtn();

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
    if (d < new Date()) return pill('expired', 'inactive');
    const diff = d - Date.now();
    const days = Math.floor(diff / 86400000);
    const hours = Math.floor((diff % 86400000) / 3600000);
    return `<span style="font-size:12px;color:var(--text-muted);" title="${d.toLocaleString()}">${days}d ${hours}h</span>`;
}

function fmtUses(u) {
    if (u === -1) return '<span style="font-size:15px;">∞</span>';
    if (u === 0) return pill('expired', '0');
    return `<span style="font-size:13px;">${u}</span>`;
}

function pill(type, text) {
    return `<span class="pill pill-${type}">${text}</span>`;
}

function showStatus(id, msg, type) {
    const el = document.getElementById(id);
    if (!el) return;
    el.textContent = msg;
    el.className = 'status-msg ' + type;
    setTimeout(() => { el.className = 'status-msg'; }, 3500);
}

// ── Generic fetch helper ───────────────────────────────
async function apiFetch(url, options = {}) {
    const res = await fetch(url, options);
    if (!res.ok) throw new Error(await res.text() || res.statusText);
    return res;
}

// ── Inline editing ────────────────────────────────────
async function updateShare(sub, patch) {
    try {
        await apiFetch(API + '?subpath=' + encodeURIComponent(sub), {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patch)
        });
        showStatus('status-shares', '/' + sub + ' updated', 'ok');
    } catch (err) {
        showStatus('status-shares', err.message, 'err');
    }
    loadShares();
}

function makeEditable(td, currentValue, type, onSave) {
    if (td.querySelector('input')) return;
    const displayHTML = td.innerHTML;
    const input = document.createElement('input');
    input.type = type;
    input.value = currentValue;
    input.style.cssText = `
        width:100%;box-sizing:border-box;
        background:var(--bg);border:1px solid var(--accent);
        border-radius:4px;color:var(--text);
        font-size:13px;padding:2px 6px;outline:none;
    `;
    td.innerHTML = '';
    td.appendChild(input);
    input.focus();
    input.select();

    function save() {
        const val = type === 'number' ? parseInt(input.value, 10) : input.value;
        (!isNaN(val) || type !== 'number') ? onSave(val) : restore();
    }
    function restore() { td.innerHTML = displayHTML; }

    input.addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); save(); }
        if (e.key === 'Escape') { e.preventDefault(); restore(); }
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
    document.getElementById('stat-total').textContent = keys.length;
    document.getElementById('stat-active').textContent = keys.length - expired.length;
    document.getElementById('stat-expired').textContent = expired.length;
    document.getElementById('stat-upload').textContent = keys.filter(k => shares[k].allow_post).length;
}

async function loadShares() {
    const tbody = document.getElementById('shares-body');
    try {
        const res = await apiFetch(API);
        const shares = await res.json();
        const keys = Object.keys(shares);
        updateStats(shares);

        if (keys.length === 0) {
            tbody.innerHTML = `<tr><td colspan="8" class="table-info"><span class="table-info-icon">📭</span>No shares yet. Add one above.</td></tr>`;
            return;
        }

        tbody.innerHTML = '';
        keys.sort().forEach(sub => {
            const s = shares[sub];
            const isExp = s.expired;
            const tr = document.createElement('tr');

            // Subpath
            const tdSub = document.createElement('td');
            tdSub.innerHTML = `<a class="subpath" href="/${sub}" target="_blank">/${sub}</a>`;

            // Lock
            const tdLock = document.createElement('td');
            tdLock.className = 'td-lock';
            tdLock.innerHTML = s.password
                ? `<span class="lock-btn" title="Password protected — click to change" onclick="editPassword('${sub}', true)">🔒</span>`
                : `<span class="lock-btn lock-btn-open" title="No password — click to add" onclick="editPassword('${sub}', false)">🔓</span>`;

            // Path (editable)
            const tdPath = document.createElement('td');
            tdPath.className = 'editable-cell';
            tdPath.title = 'Click to edit';
            tdPath.innerHTML = `<span class="path-text" title="${s.path}">${s.path}</span>`;
            tdPath.addEventListener('click', () => makeEditable(tdPath, s.path, 'text', val => {
                if (val.trim()) updateShare(sub, { path: val.trim() });
                else tdPath.innerHTML = `<span class="path-text" title="${s.path}">${s.path}</span>`;
            }));

            // Uses (editable)
            const tdUses = document.createElement('td');
            tdUses.className = 'hide-sm editable-cell';
            tdUses.title = 'Click to edit';
            tdUses.innerHTML = fmtUses(s.uses);
            tdUses.addEventListener('click', () => makeEditable(tdUses, s.uses, 'number', val => updateShare(sub, { uses: val })));

            // Expiration (editable)
            const tdExp = document.createElement('td');
            tdExp.className = 'hide-sm editable-cell';
            tdExp.title = 'Click to edit (unix timestamp, 0 = never)';
            tdExp.innerHTML = fmtExp(s.expiration);
            tdExp.addEventListener('click', () => makeEditable(tdExp, s.expiration, 'number', val => updateShare(sub, { expiration: val })));

            // Upload toggle
            const tdUpload = document.createElement('td');
            tdUpload.className = 'editable-cell';
            tdUpload.title = 'Click to toggle';
            tdUpload.style.cursor = 'pointer';
            tdUpload.innerHTML = pill(s.allow_post ? 'yes' : 'no', s.allow_post ? 'on' : 'off');
            tdUpload.addEventListener('click', () => {
                const next = !s.allow_post;
                tdUpload.innerHTML = pill(next ? 'yes' : 'no', next ? 'on' : 'off');
                updateShare(sub, { allow_post: next });
            });

            // Status toggle
            const tdStatus = document.createElement('td');
            tdStatus.className = 'editable-cell';
            tdStatus.title = 'Click to toggle';
            tdStatus.style.cursor = 'pointer';
            tdStatus.innerHTML = pill(isExp ? 'expired' : 'active', isExp ? 'expired' : 'active');
            tdStatus.addEventListener('click', () => {
                const next = !isExp;
                tdStatus.innerHTML = pill(next ? 'expired' : 'active', next ? 'expired' : 'active');
                updateShare(sub, { expired: next });
            });

            // Delete
            const tdDel = document.createElement('td');
            tdDel.innerHTML = `<button class="btn btn-danger-ghost" onclick="deleteShare('${sub}')">Delete</button>`;

            tr.append(tdSub, tdLock, tdPath, tdUses, tdExp, tdUpload, tdStatus, tdDel);
            tbody.appendChild(tr);
        });

    } catch (err) {
        tbody.innerHTML = `<tr><td colspan="8" class="table-info" style="color:var(--danger);"><span class="table-info-icon">⚠</span>Failed to load: ${err.message}</td></tr>`;
    }
}

async function addShare() {
    let subpath = document.getElementById('f-subpath').value.trim();
    const path = document.getElementById('f-path').value.trim();
    const uses = parseInt(document.getElementById('f-uses').value);
    const expiration = parseInt(document.getElementById('f-expiration').value);
    const allowPost = document.getElementById('f-allowpost').checked;
    const password = document.getElementById('f-password').value;

    if (!path) { showStatus('status-shares', 'Path is required', 'err'); return; }
    if (!subpath) {
        const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
        subpath = Array.from({ length: 12 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
    }

    const body = { subpath, path, uses, expiration, allow_post: allowPost };
    if (password) body.password = password;

    try {
        await apiFetch(API, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body)
        });
        showStatus('status-shares', '/' + subpath + ' added', 'ok');
        resetForm();
        loadShares();
    } catch (err) { showStatus('status-shares', err.message, 'err'); }
}

async function deleteShare(sub) {
    if (!confirm('Delete /' + sub + '?')) return;
    try {
        await apiFetch(API + '?subpath=' + encodeURIComponent(sub), { method: 'DELETE' });
        showStatus('status-shares', '/' + sub + ' deleted', 'ok');
        loadShares();
    } catch (err) { showStatus('status-shares', err.message, 'err'); }
}

function resetForm() {
    ['f-subpath', 'f-path', 'f-password'].forEach(id => document.getElementById(id).value = '');
    document.getElementById('f-uses').value = '-1';
    document.getElementById('f-expiration').value = '0';
    document.getElementById('f-allowpost').checked = false;
}

// ── Settings ──────────────────────────────────────────

// Generalized credential update — both username and password follow the same pattern
async function updateCredential({ url, payload, statusId, clearIds, successMsg }) {
    try {
        await apiFetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        });
        showStatus(statusId, successMsg, 'ok');
        clearIds.forEach(id => document.getElementById(id).value = '');
    } catch (err) { showStatus(statusId, err.message, 'err'); }
}

function changeUsername() {
    const current = document.getElementById('s-un-current').value;
    const username = document.getElementById('s-un-new').value.trim();
    if (!current) { showStatus('status-un', 'Current password is required', 'err'); return; }
    if (!username) { showStatus('status-un', 'Username cannot be empty', 'err'); return; }
    updateCredential({
        url: '/admin/api/settings/username',
        payload: { current_password: current, new_username: username },
        statusId: 'status-un',
        clearIds: ['s-un-current', 's-un-new'],
        successMsg: 'Username updated!'
    });
}

function changePassword() {
    const current = document.getElementById('s-pw-current').value;
    const pw1 = document.getElementById('s-pw-new').value;
    const pw2 = document.getElementById('s-pw-confirm').value;
    if (!current) { showStatus('status-pw', 'Current password is required', 'err'); return; }
    if (!pw1) { showStatus('status-pw', 'New password cannot be empty', 'err'); return; }
    if (pw1 !== pw2) { showStatus('status-pw', 'Passwords do not match', 'err'); return; }
    updateCredential({
        url: '/admin/api/settings/password',
        payload: { current_password: current, new_password: pw1 },
        statusId: 'status-pw',
        clearIds: ['s-pw-current', 's-pw-new', 's-pw-confirm'],
        successMsg: 'Password updated!'
    });
}

async function pruneExpired() {
    if (!confirm('Delete all expired shares from data.json?')) return;
    try {
        await apiFetch('/admin/api/settings/prune_expired', { method: 'POST' });
        showStatus('status-prune', 'Expired shares deleted', 'ok');
        loadShares();
    } catch (err) { showStatus('status-prune', err.message, 'err'); }
}

// ── Logs ──────────────────────────────────────────────
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
    div.dataset.level = entry.level;

    const time = entry.time.substring(11, 19);

    let parsed = null;
    try { parsed = JSON.parse(entry.message); } catch (_) { }

    if (parsed?.method && parsed?.url) {
        div.className = 'log-line-request';
        div.style.cursor = 'pointer';
        div.textContent = `[${entry.level}] ${time} — ${parsed.client_ip ?? ''}: ${parsed.method} ${parsed.url}`;

        const detail = document.createElement('pre');
        detail.textContent = JSON.stringify(parsed, null, 2);
        detail.className = 'log-detail';
        detail.style.display = 'none';
        div.appendChild(detail);
        div.addEventListener('click', () => {
            detail.style.display = detail.style.display === 'block' ? 'none' : 'block';
        });
    } else {
        div.className = LOG_CLASSES[entry.level] ?? '';
        div.textContent = `[${entry.level}] ${time} — ${entry.message}`;
    }

    box.appendChild(div);
    box.scrollTop = box.scrollHeight;
}

async function loadLogs() {
    try {
        const res = await apiFetch('/admin/api/logs?n=200');
        const entries = await res.json();
        entries.forEach(appendLogLine);
    } catch (err) {
        document.getElementById('log-box').innerHTML =
            `<span style="color:var(--danger);">Failed to load logs: ${err.message}</span>`;
    }

    if (logEventSource) logEventSource.close();
    logEventSource = new EventSource('/admin/api/logs/stream');
    logEventSource.onmessage = e => { appendLogLine(JSON.parse(e.data)); applyFilter(); };
    logEventSource.onerror = () => { console.warn('SSE connection lost, retrying...'); };
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