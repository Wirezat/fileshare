/* fileshare / admin/shares.js — the Shares page of the wui-based admin panel.

   Structure follows wui's declarative entry point: init() sets up chrome and
   i18n, renderPage() mounts one stats container and one table container, and
   every later change goes through those two handles' update().

   The table component renders cells from HTML strings (col.render), so nothing
   here can attach a listener to a cell directly — all cell interaction is
   delegated from the container element and keyed off data-* attributes.
*/

import { renderPage, showModal }        from '/static/ui/js/wui.js'
import { getLang, t }                  from '/static/ui/js/i18n.js'
import { showToast }                   from '/static/ui/js/components/toast.js'
import { attachInlineEdit }            from '/static/ui/js/inline-edit.js'
import { bootChrome, PATHS, esc, tf, apiFetch } from './chrome.js'

const API = '/admin/api/shares'

/* ── Helpers ─────────────────────────────────────────────────────────────── */

function tsToLocalInput(ts) {
    if (!ts) return ''
    const d = new Date(ts * 1000)
    d.setMinutes(d.getMinutes() - d.getTimezoneOffset())
    return d.toISOString().slice(0, 16)
}

function localInputToTs(val) {
    return val ? Math.floor(new Date(val).getTime() / 1000) : 0
}

/* A share counts as inactive if it was switched off by hand, ran out of uses,
   or passed its expiry. The server applies the same three conditions, so the
   stats cannot drift from what /admin actually serves. */
function isInactive(s) {
    return blockedBy(s) !== null
}

/* Which of the three conditions is keeping a share down, or null if none is.
   The distinction matters because only the first is something the status badge
   can undo: clearing the `expired` flag on a share whose date has passed saves
   fine and changes nothing anyone can see, which reads as a broken button. */
function blockedBy(s) {
    if (s.expired) return 'manual'
    if (s.expiration !== 0 && s.expiration < Date.now() / 1000) return 'date'
    if (s.uses === 0) return 'uses'
    return null
}

function fail(err) {
    showToast({ tone: 'danger', messageKey: tf('shares.toast.failed', { error: err.message }) })
}

/* ── State ───────────────────────────────────────────────────────────────── */

let shares = {}
let filterText = ''
let page = null

/* ── Cell renderers ──────────────────────────────────────────────────────── */

function renderSubpath(_val, row) {
    const locked = !!row.s.password
    const hint = locked ? 'shares.hint.password_set' : 'shares.hint.password_none'
    // btn-toggle is wui's frameless two-state icon button: the lock reads as
    // set/unset by intensity, without a box competing with the link next to it.
    return `<span class="cell-subpath">`
        + `<a class="td-link" href="/${esc(row.sub)}" target="_blank" rel="noopener">/${esc(row.sub)}</a>`
        + `<button class="btn btn-icon btn-sm btn-toggle" data-act="password"`
        + ` data-sub="${esc(row.sub)}" aria-pressed="${locked}"`
        + ` title="${esc(t(hint))}">${locked ? '🔒' : '🔓'}</button></span>`
}

function editableCell(row, field, text) {
    return `<span class="field-editable" data-inline data-sub="${esc(row.sub)}"`
        + ` data-field="${esc(field)}" title="${esc(t('shares.hint.edit'))}">${esc(text)}</span>`
}

function renderUses(_val, row) {
    const u = row.s.uses
    const text = u === -1 ? '∞' : String(u)
    return editableCell(row, 'uses', text)
}

function renderExpires(_val, row) {
    const ts = row.s.expiration
    if (!ts) {
        return `<span class="field-editable td-faint" data-act="expires" data-sub="${esc(row.sub)}"`
            + ` title="${esc(t('shares.hint.edit'))}">${esc(t('shares.value.never'))}</span>`
    }
    const d = new Date(ts * 1000)
    const past = d < new Date()
    // getLang(), not the browser locale: the date sits between translated
    // column headers, so it has to follow the language the page is showing.
    const label = past
        ? t('shares.value.expired')
        : d.toLocaleString(getLang(), { dateStyle: 'medium', timeStyle: 'short' })
    return `<span class="field-editable${past ? ' td-faint' : ''}" data-act="expires"`
        + ` data-sub="${esc(row.sub)}" title="${esc(d.toLocaleString())}">${esc(label)}</span>`
}

function toggleBadge(row, act, on, onKey, offKey, hintKey) {
    return `<button class="badge ${on ? 'badge-success' : ''}" data-act="${act}"`
        + ` data-sub="${esc(row.sub)}" title="${esc(t(hintKey))}">`
        + `${esc(t(on ? onKey : offKey))}</button>`
}

function renderUpload(_val, row) {
    return toggleBadge(row, 'upload', row.s.allow_post,
        'shares.value.on', 'shares.value.off', 'shares.hint.toggle_upload')
}

/* The badge always states the truth, and it is only a button when a click can
   change that truth. A share held down by its date or its use count is fixed
   in the column that shows the offending value, and the badge says so instead
   of accepting a click that would do nothing visible. */
const STATUS = {
    manual: { label: 'shares.value.off', hint: 'shares.hint.activate', clickable: true },
    date: { label: 'shares.value.expired', hint: 'shares.hint.blocked_date', clickable: false },
    uses: { label: 'shares.value.usedup', hint: 'shares.hint.blocked_uses', clickable: false },
}

function renderStatus(_val, row) {
    const blocker = blockedBy(row.s)

    if (blocker === null) {
        return `<button class="badge badge-success" data-act="status" data-sub="${esc(row.sub)}"`
            + ` title="${esc(t('shares.hint.deactivate'))}">${esc(t('shares.value.active'))}</button>`
    }

    const { label, hint, clickable } = STATUS[blocker]
    if (!clickable) {
        return `<span class="badge badge-danger" title="${esc(t(hint))}">${esc(t(label))}</span>`
    }
    return `<button class="badge badge-danger" data-act="status" data-sub="${esc(row.sub)}"`
        + ` title="${esc(t(hint))}">${esc(t(label))}</button>`
}

const COLUMNS = [
    { key: 'sub',     labelKey: 'shares.col.subpath', render: renderSubpath },
    { key: 'path',    labelKey: 'shares.col.path',    grow: true, cls: 'cell-editable td-mono',
      render: (_v, row) => editableCell(row, 'path', row.s.path) },
    { key: 'uses',    labelKey: 'shares.col.uses',    cls: 'cell-editable col-narrow', render: renderUses },
    { key: 'expires', labelKey: 'shares.col.expires', cls: 'cell-editable col-narrow', render: renderExpires },
    { key: 'upload',  labelKey: 'shares.col.upload',  render: renderUpload },
    { key: 'status',  labelKey: 'shares.col.status',  render: renderStatus },
]

/* ── Data → rows ─────────────────────────────────────────────────────────── */

function currentRows() {
    const q = filterText.trim().toLowerCase()
    return Object.keys(shares)
        .filter(sub => !q || sub.toLowerCase().includes(q)
                          || shares[sub].path.toLowerCase().includes(q))
        .sort()
        .map(sub => ({ sub, s: shares[sub] }))
}

function currentStats() {
    const all = Object.values(shares)
    const inactive = all.filter(isInactive).length
    return [
        { value: all.length,                             labelKey: 'shares.stat.total' },
        { value: all.length - inactive,                  labelKey: 'shares.stat.active' },
        { value: inactive,                               labelKey: 'shares.stat.inactive' },
        { value: all.filter(s => s.allow_post).length,   labelKey: 'shares.stat.upload' },
    ]
}

function refresh() {
    page.get('stats').update({ stats: currentStats() })
    page.get('table').update({ rows: currentRows() })
    // renderTable() replaces the tbody wholesale, so the freshly built
    // .field-editable spans have to be re-wired after every update.
    page.get('table').el
        .querySelectorAll('.field-editable[data-inline]')
        .forEach(attachInlineEdit)
}

async function loadShares() {
    try {
        shares = await (await apiFetch(API)).json()
        refresh()
    } catch (err) {
        showToast({ tone: 'danger', messageKey: tf('shares.load_failed', { error: err.message }) })
    }
}

async function patchShare(sub, patch, quiet = false) {
    try {
        await apiFetch(`${API}?subpath=${encodeURIComponent(sub)}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patch),
        })
        if (!quiet) showToast({ messageKey: tf('shares.toast.updated', { subpath: sub }) })
    } catch (err) {
        fail(err)
    }
    await loadShares()
}

/* ── Field builders for the modals ───────────────────────────────────────── */

function field(labelKey, inputHTML, hintKey) {
    const wrap = document.createElement('div')
    wrap.className = 'field'
    wrap.innerHTML = `<label class="field-label">${esc(t(labelKey))}</label>${inputHTML}`
        + (hintKey ? `<span class="field-caption">${esc(t(hintKey))}</span>` : '')
    return wrap
}

function newShareForm() {
    const node = document.createElement('div')
    node.className = 'form-stack'
    node.append(
        field('shares.form.subpath',
            `<input class="input" name="subpath" autocomplete="off">`, 'shares.form.subpath_hint'),
        field('shares.form.path',
            `<input class="input" name="path" autocomplete="off" placeholder="/srv/files/report.pdf">`),
        field('shares.form.uses',
            `<input class="input" name="uses" type="number" min="-1" value="-1">`, 'shares.form.uses_hint'),
        // Native datetime-local rather than wui's .wui-dt-* recipe: that one is
        // CSS only, so a custom picker would have to be written from scratch.
        field('shares.form.expires',
            `<input class="input" name="expiration" type="datetime-local">`, 'shares.form.expires_hint'),
        field('shares.form.password',
            `<input class="input" name="password" type="password" autocomplete="new-password">`,
            'shares.form.password_hint'),
    )
    const check = document.createElement('label')
    check.className = 'check-item'
    check.innerHTML = `<input type="checkbox" name="allow_post">`
    check.append(document.createTextNode(t('shares.form.allow_post')))
    node.appendChild(check)
    return node
}

/* ── Actions ─────────────────────────────────────────────────────────────── */

function openNewShare() {
    const node = newShareForm()
    const get = name => node.querySelector(`[name="${name}"]`)

    showModal({
        preset: 'form',
        titleKey: 'shares.new',
        content: { type: 'raw', node },
        actions: [
            { labelKey: 'common.cancel', variant: 'ghost' },
            {
                labelKey: 'common.create',
                variant: 'primary',
                closeOnClick: false,
                onClick: async () => {
                    const path = get('path').value.trim()
                    if (!path) {
                        showToast({ tone: 'danger', messageKey: t('shares.form.path_required') })
                        return
                    }
                    // Random subpath is generated client-side, as /admin does.
                    // A collision surfaces as the server's 409, which the catch
                    // below reports rather than silently overwriting anything.
                    const subpath = get('subpath').value.trim() || randomSubpath()
                    const body = {
                        subpath,
                        path,
                        uses: parseInt(get('uses').value, 10) || -1,
                        expiration: localInputToTs(get('expiration').value),
                        allow_post: get('allow_post').checked,
                    }
                    const pw = get('password').value
                    if (pw) body.password = pw

                    try {
                        await apiFetch(API, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify(body),
                        })
                        showToast({ messageKey: tf('shares.toast.created', { subpath }) })
                        closeModalNow()
                        await loadShares()
                    } catch (err) {
                        fail(err)
                    }
                },
            },
        ],
    })
}

function randomSubpath() {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
    return Array.from(crypto.getRandomValues(new Uint8Array(12)))
        .map(b => chars[b % chars.length]).join('')
}

/* The modal handle is only returned by showModal, but the create action has to
   survive a failed submit — so closing is deferred to this helper instead of
   the action's default closeOnClick. */
function closeModalNow() {
    document.querySelector('.modal-backdrop')?.remove()
}

function openPasswordModal(sub) {
    const locked = !!shares[sub]?.password
    const node = document.createElement('div')
    node.className = 'form-stack'
    node.appendChild(field('shares.password.field',
        `<input class="input" name="password" type="password" autocomplete="new-password">`,
        'shares.password.hint'))

    showModal({
        preset: 'form',
        titleKey: locked ? 'shares.password.title_change' : 'shares.password.title_set',
        content: { type: 'raw', node },
        actions: [
            { labelKey: 'common.cancel', variant: 'ghost' },
            {
                labelKey: 'common.save',
                variant: 'primary',
                onClick: () => patchShare(sub, {
                    password: node.querySelector('[name="password"]').value,
                }),
            },
        ],
    })
}

function confirmDelete(sub) {
    showModal({
        preset: 'danger-confirm',
        titleKey: 'shares.delete.title',
        messageKey: tf('shares.delete.message', { subpath: sub }),
        actions: [
            { labelKey: 'common.cancel', variant: 'ghost' },
            {
                labelKey: 'common.delete',
                variant: 'danger',
                onClick: async () => {
                    try {
                        await apiFetch(`${API}?subpath=${encodeURIComponent(sub)}`, { method: 'DELETE' })
                        showToast({ messageKey: tf('shares.toast.deleted', { subpath: sub }) })
                    } catch (err) {
                        fail(err)
                    }
                    await loadShares()
                },
            },
        ],
    })
}

/* Swaps the expiry cell for a native datetime input. Committing on both change
   and blur covers the two ways a date picker can be dismissed. */
function editExpiration(span, sub) {
    if (span.dataset.editing) return
    span.dataset.editing = '1'

    const input = document.createElement('input')
    input.type = 'datetime-local'
    input.className = 'input'
    input.value = tsToLocalInput(shares[sub].expiration)
    span.replaceWith(input)
    input.focus()

    let done = false
    const commit = () => {
        if (done) return
        done = true
        const next = localInputToTs(input.value)
        if (next === shares[sub].expiration) refresh()
        else patchShare(sub, { expiration: next })
    }
    input.addEventListener('change', commit)
    input.addEventListener('blur', commit)
    input.addEventListener('keydown', e => {
        if (e.key === 'Enter') commit()
        if (e.key === 'Escape') { done = true; refresh() }
    })
}

/* ── Wiring ──────────────────────────────────────────────────────────────── */

function wireTable(el) {
    el.addEventListener('click', e => {
        const target = e.target.closest('[data-act]')
        if (!target) return
        const sub = target.dataset.sub
        switch (target.dataset.act) {
            case 'password': openPasswordModal(sub); break
            case 'upload':   patchShare(sub, { allow_post: !shares[sub].allow_post }, true); break
            // Only reachable on shares whose state the flag actually decides —
            // renderStatus renders a plain span otherwise.
            case 'status':   patchShare(sub, { expired: !shares[sub].expired }, true); break
            case 'expires':  editExpiration(target, sub); break
        }
    })

    el.addEventListener('inline-commit', e => {
        const { sub, field: name } = e.target.dataset
        const raw = e.detail.value.trim()

        /* The local copy is updated before the request goes out, not after it
           comes back. wui's inline edit commits on Enter and again on the blur
           that follows, and while the round trip is in flight the second commit
           would still see the old value and send an identical second write. */
        if (name === 'path') {
            if (!raw || raw === shares[sub].path) return refresh()
            shares[sub].path = raw
            return patchShare(sub, { path: raw })
        }
        if (name === 'uses') {
            const n = raw === '∞' ? -1 : parseInt(raw, 10)
            if (!Number.isInteger(n) || n === shares[sub].uses) return refresh()
            shares[sub].uses = n
            return patchShare(sub, { uses: n })
        }
    })

    el.addEventListener('inline-cancel', refresh)
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

page = renderPage({
    pageHeader: {
        titleKey: 'shares.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'shares.title' }],
        actions: [{ labelKey: 'shares.new', variant: 'primary', onClick: openNewShare }],
    },
    full: [
        { id: 'stats', title: 'shares.overview', content: { type: 'tiles', stats: currentStats() } },
    ],
    main: [
        {
            id: 'table',
            title: 'shares.list',
            filters: [{ type: 'search', placeholderKey: 'shares.filter',
                        onInput: v => { filterText = v; refresh() } }],
            content: {
                type: 'table',
                columns: COLUMNS,
                rows: [],
                emptyMessageKey: 'shares.empty',
                rowActions: [{ labelKey: 'common.delete', onClick: row => confirmDelete(row.sub) }],
            },
        },
    ],
})

wireTable(page.get('table').el)
loadShares()
