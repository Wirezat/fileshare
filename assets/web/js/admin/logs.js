/* fileshare / admin/logs.js — the live server log.

   Two sources feed one view: a REST call for the backlog the server already
   holds, then an SSE stream for everything after it. Both land in appendEntry,
   so a line looks the same whether it arrived by history or by stream.
*/

import { renderPage }        from '/static/ui/js/wui.js'
import { t }                 from '/static/ui/js/i18n.js'
import { showToast }         from '/static/ui/js/components/toast.js'
import { bootChrome, PATHS, esc, tf, apiFetch } from './chrome.js'

/* Levels the Go logger actually emits, taken from the log file's [LEVEL]
   prefix. DEBUG is included deliberately: /admin's viewer offers only
   INFO/WARN/ERROR checkboxes and its filter hides every level that is not
   checked, so DEBUG lines are unreachable there. */
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR']

/* A long-lived admin tab on a busy server would otherwise grow without bound —
   the server itself only keeps 500 entries. */
const MAX_LINES = 2000

const state = {
    levels: new Set(LEVELS),
    search: '',
    box: null,
    source: null,
}

/* ── One log line ────────────────────────────────────────────────────────── */

/* Request logs are JSON written by loggingMiddleware; everything else is a
   plain message. Returns null for a non-request line. */
function parseRequest(message) {
    try {
        const p = JSON.parse(message)
        return (p && p.method && p.url) ? p : null
    } catch {
        return null
    }
}

function buildLine(entry) {
    const div = document.createElement('div')
    div.className = `log-line log-${entry.level.toLowerCase()}`
    div.dataset.level = entry.level

    const time = entry.time.substring(11, 19)
    const req = parseRequest(entry.message)

    if (req) {
        div.classList.add('log-request')
        const summary = `${req.client_ip ?? '?'} — ${req.method} ${req.url}`
        div.innerHTML =
            `<span class="log-level">${esc(entry.level)}</span>`
            + `<span class="log-time">${esc(time)}</span>`
            + `<span class="log-msg">${esc(summary)}</span>`
        // Full request JSON, hidden until the line is clicked.
        const detail = document.createElement('pre')
        detail.className = 'log-detail'
        detail.hidden = true
        detail.textContent = JSON.stringify(req, null, 2)
        div.appendChild(detail)
        div.addEventListener('click', () => { detail.hidden = !detail.hidden })
        div.dataset.text = `${entry.level} ${time} ${summary}`.toLowerCase()
    } else {
        div.innerHTML =
            `<span class="log-level">${esc(entry.level)}</span>`
            + `<span class="log-time">${esc(time)}</span>`
            + `<span class="log-msg">${esc(entry.message)}</span>`
        div.dataset.text = `${entry.level} ${time} ${entry.message}`.toLowerCase()
    }

    return div
}

function matches(div) {
    if (!state.levels.has(div.dataset.level)) return false
    if (state.search && !div.dataset.text.includes(state.search)) return false
    return true
}

/* Only follow the tail if the reader is already at it — scrolling up to read
   history should not be yanked back down by the next incoming line. */
function atBottom(box) {
    return box.scrollHeight - box.scrollTop - box.clientHeight < 40
}

function appendEntry(entry) {
    const box = state.box
    const stick = atBottom(box)

    const div = buildLine(entry)
    div.hidden = !matches(div)
    box.appendChild(div)

    while (box.childElementCount > MAX_LINES) box.firstElementChild.remove()
    if (stick) box.scrollTop = box.scrollHeight
}

function applyFilter() {
    for (const div of state.box.children) div.hidden = !matches(div)
}

/* ── Body ────────────────────────────────────────────────────────────────── */

function buildBody() {
    const node = document.createElement('div')
    node.className = 'log-panel'

    const bar = document.createElement('div')
    bar.className = 'log-toolbar'

    for (const level of LEVELS) {
        const label = document.createElement('label')
        label.className = 'check-item'
        const cb = document.createElement('input')
        cb.type = 'checkbox'
        cb.checked = true
        cb.addEventListener('change', () => {
            cb.checked ? state.levels.add(level) : state.levels.delete(level)
            applyFilter()
        })
        label.append(cb, document.createTextNode(t(`logs.level.${level.toLowerCase()}`)))
        bar.appendChild(label)
    }

    const spacer = document.createElement('span')
    spacer.className = 'log-toolbar-spacer'
    bar.appendChild(spacer)

    const clear = document.createElement('button')
    clear.className = 'btn btn-ghost btn-sm'
    clear.textContent = t('logs.clear')
    clear.title = t('logs.clear_hint')
    clear.addEventListener('click', () => {
        state.box.innerHTML = ''
        showToast({ messageKey: t('logs.cleared') })
    })
    bar.appendChild(clear)

    state.box = document.createElement('div')
    state.box.className = 'log-box'

    node.append(bar, state.box)
    return node
}

/* ── Sources ─────────────────────────────────────────────────────────────── */

async function loadHistory() {
    try {
        const entries = await (await apiFetch('/admin/api/logs?n=200')).json()
        entries.forEach(appendEntry)
        state.box.scrollTop = state.box.scrollHeight
    } catch (err) {
        showToast({ tone: 'danger', messageKey: tf('logs.load_failed', { error: err.message }) })
    }
}

function openStream() {
    state.source?.close()
    state.source = new EventSource('/admin/api/logs/stream')
    state.source.onmessage = e => {
        try {
            appendEntry(JSON.parse(e.data))
        } catch (err) {
            console.warn('log stream: unparseable entry', err)
        }
    }
    // EventSource reconnects on its own; entries produced while the connection
    // was down are lost, which a page reload recovers from the server's buffer.
    state.source.onerror = () => console.warn('log stream: connection lost, retrying')
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

renderPage({
    pageHeader: {
        titleKey: 'logs.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'logs.title' }],
    },
    main: [
        {
            id: 'log',
            title: 'logs.stream',
            filters: [{ type: 'search', placeholderKey: 'logs.filter',
                        onInput: v => { state.search = v.trim().toLowerCase(); applyFilter() } }],
            content: { type: 'raw', node: buildBody() },
        },
    ],
})

await loadHistory()
openStream()
