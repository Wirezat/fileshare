/* fileshare / admin/stats.js — downloads, uploads and visitors, all time or the last 30 days. */

import { renderPage }        from '/static/ui/js/wui.js'
import { getLang, t }        from '/static/ui/js/i18n.js'
import { showToast }         from '/static/ui/js/components/toast.js'
import { bootChrome, PATHS, esc, tf, apiFetch } from './chrome.js'

const REFRESH_MS = 5_000

const state = { period: 'recent', report: null, root: null, failing: false }

function el(tag, cls, text) {
    const e = document.createElement(tag)
    if (cls) e.className = cls
    if (text != null) e.textContent = text
    return e
}

const num = n => new Intl.NumberFormat(getLang()).format(n)
const pct = (part, whole) => whole > 0
    ? new Intl.NumberFormat(getLang(), { style: 'percent', maximumFractionDigits: 1 }).format(part / whole)
    : '–'
const asDate = d => /^\d{4}-\d{2}-\d{2}$/.test(d) ? new Date(+d.slice(0, 4), +d.slice(5, 7) - 1, +d.slice(8, 10)) : new Date(d)
const day = d => asDate(d).toLocaleDateString(getLang(), { day: '2-digit', month: '2-digit', year: 'numeric' })
const shortDay = d => asDate(d).toLocaleDateString(getLang(), { day: 'numeric', month: 'numeric' })

function bytes(n) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let i = 0
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
    const digits = i === 0 || n >= 100 ? 0 : 1
    return `${new Intl.NumberFormat(getLang(), { maximumFractionDigits: digits }).format(n)} ${units[i]}`
}

function ago(iso) {
    if (!iso || iso.startsWith('0001')) return '–'
    const secs = (new Date(iso) - Date.now()) / 1000
    const rtf = new Intl.RelativeTimeFormat(getLang(), { numeric: 'auto' })
    for (const [unit, size] of [['day', 86400], ['hour', 3600], ['minute', 60]]) {
        if (Math.abs(secs) >= size) return rtf.format(Math.round(secs / size), unit)
    }
    return rtf.format(0, 'minute')
}

/* ── Tiles ───────────────────────────────────────────────────────────────── */

function tile(label, value, sub, live = false) {
    const box = el('div', 'stat-tile')
    box.appendChild(el('span', 'stat-tile-label', label))
    const v = el('span', 'stat-tile-value')
    if (live) v.appendChild(el('span', 'live-dot'))
    v.appendChild(document.createTextNode(value))
    box.append(v, el('span', 'stat-tile-sub', sub))
    return box
}

function tiles(r, p) {
    const grid = el('div', 'stat-grid')
    const today = r.days[r.days.length - 1]
    const finishedUploads = p.uploads + p.uploadsAbandoned
    const peak = p.peak.n > 0 ? tf('stats.tile.peak', { n: num(p.peak.n), date: shortDay(p.peak.at) }) : t('stats.tile.no_peak')
    grid.append(
        tile(t('stats.tile.visitors'), num(p.visitors),
            state.period === 'recent' ? tf('stats.tile.visitors_today', { n: num(today.visitors) }) : t('stats.tile.visitors_all')),
        tile(t('stats.tile.downloads'), num(p.downloads), tf('stats.tile.zip', { n: num(p.zipDownloads) })),
        tile(t('stats.tile.uploads'), num(p.uploads), t('stats.tile.files')),
        tile(t('stats.tile.down'), bytes(p.bytesDown), '↓'),
        tile(t('stats.tile.up'), bytes(p.bytesUp), '↑'),
        tile(t('stats.tile.aborts'),
            pct(p.downloadsIncomplete + p.uploadsAbandoned, p.downloads + finishedUploads),
            tf('stats.tile.aborts_split', {
                down: pct(p.downloadsIncomplete, p.downloads),
                up: pct(p.uploadsAbandoned, finishedUploads),
            })),
        tile(t('stats.tile.live'), tf('stats.tile.now', { n: num(r.live) }), peak, true),
    )
    return grid
}

/* ── Containers ──────────────────────────────────────────────────────────── */

function container(title, body, { hint, flush = false } = {}) {
    const box = el('div', 'container')
    const head = el('div', 'container-header')
    head.appendChild(el('span', 'container-title', title))
    if (hint) head.appendChild(hint instanceof Node ? hint : el('span', 'stats-hint', hint))
    const inner = el('div', flush ? 'container-body-flush table-scroll' : 'container-body')
    inner.appendChild(body)
    box.append(head, inner)
    return box
}

function two(...children) {
    const row = el('div', 'stats-two')
    row.append(...children)
    return row
}

/* ── Daily chart ─────────────────────────────────────────────────────────── */

/* Returns the smallest four times a round whole step that is at least v. */
function niceMax(v) {
    const need = Math.max(1, v / 4)
    const pow = 10 ** Math.floor(Math.log10(need))
    const step = [1, 2, 3, 4, 5, 6, 8, 10].map(m => m * pow).find(x => x >= need)
    return 4 * step
}

function dailyChart(days) {
    const W = 720, H = 200, L = 36, R = 16, B = 24, T = 10
    const max = niceMax(Math.max(...days.flatMap(d => [d.downloads, d.uploads, d.visitors])))
    const y = v => T + (H - T - B) * (1 - v / max)
    const bw = (W - L - R) / days.length
    let s = ''
    for (const g of [0, 0.25, 0.5, 0.75, 1].map(f => f * max)) {
        s += `<line class="grid" x1="${L}" x2="${W - R}" y1="${y(g)}" y2="${y(g)}" />`
        s += `<text x="${L - 6}" y="${y(g) + 4}" text-anchor="end">${esc(num(g))}</text>`
    }
    days.forEach((d, i) => {
        const x = L + i * bw + 2, w = (bw - 4) / 2
        s += `<rect class="bar-down" x="${x}" y="${y(d.downloads)}" width="${w}" height="${y(0) - y(d.downloads)}" rx="1.5">`
            + `<title>${esc(shortDay(d.date))}: ${esc(tf('stats.daily.downloads_n', { n: num(d.downloads) }))}</title></rect>`
        s += `<rect class="bar-up" x="${x + w}" y="${y(d.uploads)}" width="${w}" height="${y(0) - y(d.uploads)}" rx="1.5">`
            + `<title>${esc(shortDay(d.date))}: ${esc(tf('stats.daily.uploads_n', { n: num(d.uploads) }))}</title></rect>`
        if (i % 5 === 0 || i === days.length - 1) {
            s += `<text x="${x + w}" y="${H - 6}" text-anchor="middle">${esc(shortDay(d.date))}</text>`
        }
    })
    const pts = days.map((d, i) => `${L + i * bw + bw / 2},${y(d.visitors)}`).join(' ')
    s += `<polyline class="line-visitors" points="${pts}" />`

    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    svg.setAttribute('class', 'chart')
    svg.setAttribute('viewBox', `0 0 ${W} ${H}`)
    svg.setAttribute('role', 'img')
    svg.setAttribute('aria-label', t('stats.daily.aria'))
    svg.innerHTML = s
    const wrap = el('div', 'chart-wrap')
    wrap.appendChild(svg)
    return wrap
}

function legend() {
    const box = el('div', 'legend')
    for (const [cls, key] of [['down', 'stats.daily.downloads'], ['up', 'stats.daily.uploads'], ['visitors', 'stats.daily.visitors']]) {
        const item = el('span')
        item.append(el('i', cls), document.createTextNode(t(key)))
        box.appendChild(item)
    }
    return box
}

/* ── Heatmap, speed, bars, tables ────────────────────────────────────────── */

function heatmap(rows) {
    const box = el('div', 'heat')
    const names = t('stats.heat.days').split(',')
    const max = Math.max(1, ...rows.flat())
    box.appendChild(el('b'))
    for (let h = 0; h < 24; h++) box.appendChild(el('b', null, h % 6 === 0 ? String(h) : ''))
    rows.forEach((row, d) => {
        box.appendChild(el('em', null, names[d]))
        row.forEach((n, h) => {
            const c = el('span')
            c.style.opacity = (0.06 + 0.94 * n / max).toFixed(2)
            c.title = tf('stats.heat.cell', { day: names[d], hour: h, n: num(n) })
            box.appendChild(c)
        })
    })
    const wrap = el('div', 'chart-wrap')
    wrap.appendChild(box)
    return wrap
}

const mbps = v => `${new Intl.NumberFormat(getLang(), { maximumFractionDigits: 1 }).format(v / (1 << 20))} MB/s`

function speed(p) {
    const box = el('div', 'speed')
    const cap = Math.max(p.speedDown.max, p.speedUp.max, 1)
    for (const [key, sp] of [['stats.speed.down', p.speedDown], ['stats.speed.up', p.speedUp]]) {
        box.appendChild(el('span', null, t(key)))
        const col = el('div')
        if (sp.count === 0) {
            col.appendChild(el('span', 'stats-hint', t('stats.speed.none')))
        } else {
            const scale = el('div', 'speed-scale')
            const range = el('div', 'speed-range')
            range.style.left = `${sp.min / cap * 100}%`
            range.style.width = `${Math.max(0.5, (sp.max - sp.min) / cap * 100)}%`
            const avg = el('div', 'speed-avg')
            avg.style.left = `calc(${sp.avg / cap * 100}% - 1.5px)`
            scale.append(range, avg)
            const lab = el('div', 'speed-label')
            lab.append(el('span', null, `min ${mbps(sp.min)}`), el('span', null, `Ø ${mbps(sp.avg)}`),
                el('span', null, `max ${mbps(sp.max)}`))
            col.append(scale, lab)
            col.title = tf('stats.speed.count', { n: num(sp.count) })
        }
        box.appendChild(col)
    }
    return box
}

function bars(counts, keys, prefix) {
    const box = el('div', 'bars')
    const total = keys.reduce((sum, k) => sum + (counts[k] ?? 0), 0)
    if (total === 0) {
        box.appendChild(el('span', 'stats-hint', t('stats.empty')))
        return box
    }
    for (const k of keys) {
        const n = counts[k] ?? 0
        const row = el('div', 'bar-row')
        row.title = num(n)
        row.appendChild(el('span', null, t(`${prefix}.${k}`)))
        const track = el('div', 'bar-track')
        const fill = el('div', k === 'bot' ? 'bar-fill bot' : 'bar-fill')
        fill.style.width = `${n / total * 100}%`
        track.appendChild(fill)
        row.append(track, el('span', 'num', pct(n, total)))
        box.appendChild(row)
    }
    return box
}

function table(heads, rows) {
    const tbl = el('table', 'table')
    const tr = el('tr')
    for (const [label, cls] of heads) tr.appendChild(el('th', cls, label))
    tbl.appendChild(el('thead')).appendChild(tr)
    const body = tbl.appendChild(el('tbody'))
    if (rows.length === 0) {
        const empty = el('td', 'muted', t('stats.empty'))
        empty.colSpan = heads.length
        body.appendChild(el('tr')).appendChild(empty)
    }
    for (const cells of rows) {
        const row = el('tr')
        for (const [text, cls] of cells) row.appendChild(el('td', cls, text))
        body.appendChild(row)
    }
    return tbl
}

const shareName = (name, deleted) => deleted ? `/${name} (${t('stats.deleted')})` : `/${name}`

function topShares(p) {
    return table(
        [[t('stats.col.share')], [t('stats.col.downloads'), 'n'], [t('stats.col.visitors'), 'n'], [t('stats.col.last'), 'n']],
        p.topShares.map(s => [[shareName(s.name, s.deleted)], [num(s.downloads), 'n'], [num(s.visitors), 'n'], [ago(s.lastAccess), 'n muted']]))
}

function topFiles(p) {
    return table(
        [[t('stats.col.file')], [t('stats.col.share')], [t('stats.col.downloads'), 'n']],
        p.topFiles.map(f => [[f.path.replace(/^\//, '')], [shareName(f.share, f.deleted), 'muted'], [num(f.downloads), 'n']]))
}

/* ── Page ────────────────────────────────────────────────────────────────── */

function subtitle(r) {
    if (state.period === 'total') return tf('stats.since', { date: day(r.since) })
    return tf('stats.window', { from: day(r.days[0].date), to: day(r.days[r.days.length - 1].date) })
}

function render() {
    const r = state.report
    if (!r?.days?.length) return
    const p = r[state.period]
    const page = el('div', 'stats-page')
    page.append(
        el('p', 'stats-hint', subtitle(r)),
        tiles(r, p),
        container(t('stats.daily.title'), dailyChart(r.days), { hint: legend() }),
        two(
            container(t('stats.heat.title'), heatmap(p.heatmap), { hint: t('stats.heat.hint') }),
            container(t('stats.speed.title'), speed(p), { hint: t('stats.speed.hint') }),
        ),
        two(
            container(t('stats.top_shares'), topShares(p), { flush: true, hint: t('stats.visitors_hint') }),
            container(t('stats.top_files'), topFiles(p), { flush: true }),
        ),
        two(
            container(t('stats.devices'), bars(p.devices, ['desktop', 'mobile', 'tablet', 'bot'], 'stats.device')),
            container(t('stats.browsers'), bars(p.browsers, ['chrome', 'safari', 'firefox', 'edge', 'other'], 'stats.browser')),
        ),
    )
    state.root.replaceChildren(page)
    const daily = page.querySelector('.chart-wrap')
    daily.scrollLeft = daily.scrollWidth
}

function periodPicker() {
    const box = el('div', 'view-picker stats-period')
    box.setAttribute('role', 'group')
    box.setAttribute('aria-label', t('stats.period'))
    const buttons = [['total', 'stats.total'], ['recent', 'stats.recent']].map(([period, key]) => {
        const btn = el('button', 'view-picker-btn', t(key))
        btn.type = 'button'
        btn.addEventListener('click', () => {
            state.period = period
            buttons.forEach(b => b.classList.toggle('active', b === btn))
            render()
        })
        btn.classList.toggle('active', period === state.period)
        return btn
    })
    box.append(...buttons)
    return box
}

async function load() {
    try {
        state.report = await (await apiFetch('/admin/api/stats')).json()
        state.failing = false
        render()
    } catch (err) {
        if (!state.failing) showToast({ tone: 'danger', messageKey: tf('stats.load_failed', { error: err.message }) })
        state.failing = true
    }
}

await bootChrome()

renderPage({
    pageHeader: {
        titleKey: 'stats.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'stats.title' }],
    },
})

const header = document.querySelector('#wui-page-content .page-header')
let actions = header?.querySelector('.page-header-actions')
if (header && !actions) {
    actions = el('div', 'page-header-actions')
    header.appendChild(actions)
}
actions?.appendChild(periodPicker())
state.root = el('div')
document.getElementById('wui-page-content').appendChild(state.root)

await load()
setInterval(() => { if (!document.hidden) load() }, REFRESH_MS)
