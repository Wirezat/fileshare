/* fileshare / admin/logs.js — the live server log.
   REST backlog first, then an SSE stream; both feed appendEntry.
*/

import { renderPage }        from '/static/ui/js/wui.js'
import { t }                 from '/static/ui/js/i18n.js'
import { showToast }         from '/static/ui/js/components/toast.js'
import { bootChrome, PATHS, esc, tf, apiFetch } from './chrome.js'

/* Levels the Go logger emits, as in the log file's [LEVEL] prefix. */
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR']

const MAX_LINES = 2000

const state = {
    levels: new Set(LEVELS),
    search: '',
    box: null,
    source: null,
}

/* ── One log line ────────────────────────────────────────────────────────── */

function buildLine(entry) {
    const div = document.createElement('div')
    div.className = `log-line log-${entry.level.toLowerCase()}`
    div.dataset.level = entry.level

    const time = entry.time.substring(11, 19)
    const req = entry.request

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

/* Whether the box is scrolled to its tail. */
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
