/* fileshare / admin/shares.js — the Shares page of the admin panel.
   Cell interaction is delegated from the container element via data-* attributes.
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

/* Inactive = switched off, or past its expiry (same rule as the server). */
function isInactive(s) {
    return blockedBy(s) !== null
}

/* Which condition is keeping a share down ('flag' | 'date'), or null. */
function blockedBy(s) {
    if (s.expired) return 'manual'
    if (s.expiration !== 0 && s.expiration < Date.now() / 1000) return 'date'
    return null
}

function fail(err) {
    showToast({ tone: 'danger', messageKey: tf('shares.toast.failed', { error: err.message }) })
}

/* ── State ───────────────────────────────────────────────────────────────── */

let shares = {}
let filterText = ''
let page = null
let officeReady = false

const OFFICE_CYCLE = { '': 'view', view: 'edit', edit: '' }

/* ── Cell renderers ──────────────────────────────────────────────────────── */

function renderSubpath(_val, row) {
    return `<a class="td-link" href="/${esc(row.sub)}" target="_blank" rel="noopener">/${esc(row.sub)}</a>`
}

function renderCopy(_val, row) {
    return `<button class="btn btn-icon btn-sm btn-icon-color" style="--_icon-color:var(--text-muted)"`
        + ` data-act="copy" data-sub="${esc(row.sub)}" title="${esc(t('shares.hint.copy_link'))}">⎘</button>`
}

function renderQr(_val, row) {
    return `<button class="btn btn-icon btn-sm btn-icon-color" style="--_icon-color:var(--text-muted)"`
        + ` data-act="qr" data-sub="${esc(row.sub)}" title="${esc(t('shares.hint.qr'))}">▦</button>`
}

function renderPassword(_val, row) {
    const locked = !!row.s.password
    const hint = locked ? 'shares.hint.password_set' : 'shares.hint.password_none'
    return `<button class="btn btn-icon btn-sm btn-toggle" data-act="password"`
        + ` data-sub="${esc(row.sub)}" aria-pressed="${locked}"`
        + ` title="${esc(t(hint))}">${locked ? '🔒' : '🔓'}</button>`
}

function renderActions(_val, row) {
    return `<button class="btn btn-icon btn-sm btn-icon-color" style="--_icon-color:var(--danger)"`
        + ` data-act="delete" data-sub="${esc(row.sub)}" title="${esc(t('common.delete'))}">🗑</button>`
}

function editableCell(row, field, text) {
    return `<span class="field-editable" data-inline data-sub="${esc(row.sub)}"`
        + ` data-field="${esc(field)}" title="${esc(t('shares.hint.edit'))}">${esc(text)}</span>`
}

function renderExpires(_val, row) {
    const ts = row.s.expiration
    if (!ts) {
        return `<span class="field-editable td-faint" data-act="expires" data-sub="${esc(row.sub)}"`
            + ` title="${esc(t('shares.hint.edit'))}">${esc(t('shares.value.never'))}</span>`
    }
    const d = new Date(ts * 1000)
    const past = d < new Date()
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

function renderZip(_val, row) {
    return toggleBadge(row, 'zip', !row.s.no_zip,
        'shares.value.on', 'shares.value.off', 'shares.hint.toggle_zip')
}

function renderOffice(_val, row) {
    const mode = row.s.office || ''
    const label = t(`shares.value.office_${mode || 'off'}`)
    if (!officeReady) {
        return `<span class="badge td-faint" title="${esc(t('shares.hint.office_unconfigured'))}">${esc(label)}</span>`
    }
    return `<button class="badge ${mode ? 'badge-success' : ''}" data-act="office"`
        + ` data-sub="${esc(row.sub)}" title="${esc(t('shares.hint.cycle_office'))}">${esc(label)}</button>`
}

/* Status badge variants; only the flag-controlled states render as a button. */
const STATUS = {
    manual: { label: 'shares.value.off', hint: 'shares.hint.activate', clickable: true },
    date: { label: 'shares.value.expired', hint: 'shares.hint.blocked_date', clickable: false },
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
    { key: 'sub',      labelKey: 'shares.col.subpath', cls: 'col-narrow', render: renderSubpath },
    { key: 'copy',     labelKey: 'shares.col.copy',    cls: 'col-narrow', render: renderCopy },
    { key: 'qr',       labelKey: 'shares.col.qr',      cls: 'col-narrow', render: renderQr },
    { key: 'password', labelKey: 'shares.col.password', cls: 'col-narrow', render: renderPassword },
    { key: 'path',     labelKey: 'shares.col.path',    grow: true, cls: 'cell-editable td-mono',
      render: (_v, row) => editableCell(row, 'path', row.s.path) },
    { key: 'expires', labelKey: 'shares.col.expires', cls: 'cell-editable col-narrow', render: renderExpires },
    { key: 'upload',  labelKey: 'shares.col.upload',  render: renderUpload },
    { key: 'zip',     labelKey: 'shares.col.zip',     render: renderZip },
    { key: 'office',  labelKey: 'shares.col.office',  render: renderOffice },
    { key: 'status',  labelKey: 'shares.col.status',  render: renderStatus },
    { key: 'actions', labelKey: 'shares.col.actions', cls: 'col-narrow', render: renderActions },
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
    // update() rebuilds the tbody, so the inline-edit spans are re-wired here.
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
        field('shares.form.expires',
            `<input class="input" name="expiration" type="datetime-local">`, 'shares.form.expires_hint'),
        field('shares.form.password',
            `<input class="input" name="password" type="password" autocomplete="new-password">`,
            'shares.form.password_hint'),
    )
    node.append(
        checkItem('allow_post', 'shares.form.allow_post', false),
        checkItem('allow_zip', 'shares.form.allow_zip', true),
        officeField(),
    )
    return node
}

function checkItem(name, labelKey, checked) {
    const label = document.createElement('label')
    label.className = 'check-item'
    label.innerHTML = `<input type="checkbox" name="${esc(name)}"${checked ? ' checked' : ''}>`
    label.append(document.createTextNode(t(labelKey)))
    return label
}

function officeField() {
    const opts = ['', 'view', 'edit'].map(mode =>
        `<button type="button" class="radio-opt${mode ? '' : ' active'}" data-office="${mode}"`
        + `${officeReady || !mode ? '' : ' disabled'}>${esc(t(`shares.value.office_${mode || 'off'}`))}</button>`,
    ).join('')
    const wrap = field('shares.form.office', `<div class="radio-group" data-office-group>${opts}</div>`,
        officeReady ? null : 'shares.hint.office_unconfigured')
    const group = wrap.querySelector('[data-office-group]')
    group.addEventListener('click', e => {
        const btn = e.target.closest('[data-office]')
        if (!btn || btn.disabled) return
        group.querySelectorAll('.radio-opt').forEach(b => b.classList.toggle('active', b === btn))
    })
    return wrap
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
                    const subpath = get('subpath').value.trim() || randomSubpath()
                    const body = {
                        subpath,
                        path,
                        expiration: localInputToTs(get('expiration').value),
                        allow_post: get('allow_post').checked,
                        no_zip: !get('allow_zip').checked,
                        office: node.querySelector('[data-office].active').dataset.office,
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

/* Closes the open modal; used by actions that must stay open on a failed submit. */
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

async function copyShareLink(sub) {
    const url = `${location.origin}/${sub}`
    try {
        await navigator.clipboard.writeText(url)
        showToast({ messageKey: t('shares.toast.link_copied') })
    } catch (err) {
        fail(err)
    }
}

function openQrModal(sub) {
    const node = document.createElement('div')
    node.className = 'form-stack qr-modal'
    const img = document.createElement('img')
    img.src = `${API}/qr?subpath=${encodeURIComponent(sub)}`
    img.alt = `/${sub}`
    node.appendChild(img)

    showModal({
        preset: 'form',
        titleKey: 'shares.qr.title',
        content: { type: 'raw', node },
        actions: [{ labelKey: 'common.close', variant: 'ghost' }],
    })
}

/* Swaps the expiry cell for a native datetime input; commits on change and blur. */
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
            case 'zip':      patchShare(sub, { no_zip: !shares[sub].no_zip }, true); break
            case 'office':   patchShare(sub, { office: OFFICE_CYCLE[shares[sub].office || ''] }, true); break
            case 'status':   patchShare(sub, { expired: !shares[sub].expired }, true); break
            case 'expires':  editExpiration(target, sub); break
            case 'copy':     copyShareLink(sub); break
            case 'qr':       openQrModal(sub); break
            case 'delete':   confirmDelete(sub); break
        }
    })

    el.addEventListener('inline-commit', e => {
        const { sub, field: name } = e.target.dataset
        const raw = e.detail.value.trim()

        // Local copy is updated before the request, so the blur commit that
        // follows Enter sees the new value and does not send a second write.
        if (name === 'path') {
            if (!raw || raw === shares[sub].path) return refresh()
            shares[sub].path = raw
            return patchShare(sub, { path: raw })
        }
    })

    el.addEventListener('inline-cancel', refresh)
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

officeReady = await apiFetch('/admin/api/settings/office')
    .then(r => r.json())
    .then(o => Boolean(o.office_url && o.secret_set))
    .catch(() => false)

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
            },
        },
    ],
})

wireTable(page.get('table').el)
loadShares()
