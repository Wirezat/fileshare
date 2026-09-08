/* fileshare / admin/settings.js — admin credentials and destructive upkeep.

   Three containers, each a self-contained form: username, password, and the
   danger zone. wui's form content-type covers text/select/checkbox but not
   password, so the field markup is built here from wui's form classes — the
   same approach the share modals use.
*/

import { renderPage, showModal } from '/static/ui/js/wui.js'
import { t }                     from '/static/ui/js/i18n.js'
import { showToast }             from '/static/ui/js/components/toast.js'
import { bootChrome, PATHS, esc, tf, apiFetch } from './chrome.js'

/* ── Field helpers ───────────────────────────────────────────────────────── */

function field(labelKey, inputHTML, captionKey) {
    const wrap = document.createElement('div')
    wrap.className = 'field'
    wrap.innerHTML = `<label class="field-label">${esc(t(labelKey))}</label>${inputHTML}`
        + (captionKey ? `<span class="field-caption">${esc(t(captionKey))}</span>` : '')
    return wrap
}

function passwordInput(name) {
    return `<input class="input" type="password" name="${esc(name)}" autocomplete="off">`
}

function ok(messageKey) {
    showToast({ messageKey: t(messageKey) })
}

function fail(err) {
    showToast({ tone: 'danger', messageKey: tf('settings.failed', { error: err.message }) })
}

/* Posts a credential change and clears the form on success. The current
   password is always required by the server; checking it here too keeps the
   round trip out of the obvious mistake. */
async function submitCredential(node, url, payload, doneKey) {
    try {
        await apiFetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        })
        node.querySelectorAll('input').forEach(i => { i.value = '' })
        node.querySelectorAll('.end-icon').forEach(i => { i.textContent = '' })
        ok(doneKey)
    } catch (err) {
        fail(err)
    }
}

function actions(labelKey, onClick) {
    const bar = document.createElement('div')
    bar.className = 'form-actions'
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.className = 'btn btn-primary'
    btn.textContent = t(labelKey)
    btn.addEventListener('click', onClick)
    bar.appendChild(btn)
    return bar
}

function description(key) {
    const p = document.createElement('p')
    p.className = 'field-caption settings-desc'
    p.textContent = t(key)
    return p
}

/* ── Username ────────────────────────────────────────────────────────────── */

function usernameForm() {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    node.append(
        description('settings.username.desc'),
        field('settings.current', passwordInput('current')),
        field('settings.username.new', `<input class="input" name="username" autocomplete="off">`),
        actions('settings.username.submit', () => {
            const current = get('current').value
            const username = get('username').value.trim()
            if (!current) return showToast({ tone: 'danger', messageKey: t('settings.current_required') })
            if (!username) return showToast({ tone: 'danger', messageKey: t('settings.username.empty') })
            submitCredential(node, '/admin/api/settings/username',
                { current_password: current, new_username: username }, 'settings.username.done')
        }),
    )
    return node
}

/* ── Password ────────────────────────────────────────────────────────────── */

function passwordForm() {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    // .input-wrap + .end-icon is wui's recipe for an in-field state marker;
    // the icon is pointer-events:none so it never blocks the input.
    const confirmField = field('settings.password.confirm',
        `<div class="input-wrap">`
        + `<input class="input has-end-icon" type="password" name="confirm" autocomplete="off">`
        + `<span class="end-icon" data-match></span></div>`)

    node.append(
        description('settings.password.desc'),
        field('settings.current', passwordInput('current')),
        field('settings.password.new', passwordInput('next')),
        confirmField,
        actions('settings.password.submit', () => {
            const current = get('current').value
            const next = get('next').value
            const confirm = get('confirm').value
            if (!current) return showToast({ tone: 'danger', messageKey: t('settings.current_required') })
            if (!next) return showToast({ tone: 'danger', messageKey: t('settings.password.empty') })
            if (next !== confirm) return showToast({ tone: 'danger', messageKey: t('settings.password.mismatch') })
            submitCredential(node, '/admin/api/settings/password',
                { current_password: current, new_password: next }, 'settings.password.done')
        }),
    )

    const marker = node.querySelector('[data-match]')
    const check = () => {
        const next = get('next').value
        const confirm = get('confirm').value
        marker.className = 'end-icon'
        marker.textContent = ''
        if (!confirm) return
        const same = next === confirm
        marker.classList.add(same ? 'ok' : 'bad')
        marker.textContent = same ? '✓' : '✗'
    }
    get('next').addEventListener('input', check)
    get('confirm').addEventListener('input', check)

    return node
}

/* ── Danger zone ─────────────────────────────────────────────────────────── */

function dangerZone() {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'

    // .alert is wui's inline banner: card surface with a semantic border.
    // A tinted background would break the library's "never a tint as surface"
    // rule, which alert.css calls out explicitly.
    const alert = document.createElement('div')
    alert.className = 'alert danger'
    alert.innerHTML = `<span class="alert-icon">⚠</span><span>${esc(t('settings.danger.prune_warning'))}</span>`

    const bar = document.createElement('div')
    bar.className = 'form-actions'
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.className = 'btn btn-danger'
    btn.textContent = t('settings.danger.prune')
    btn.addEventListener('click', confirmPrune)
    bar.appendChild(btn)

    node.append(alert, bar)
    return node
}

function confirmPrune() {
    showModal({
        preset: 'danger-confirm',
        titleKey: 'settings.danger.title',
        messageKey: 'settings.danger.prune_confirm',
        actions: [
            { labelKey: 'common.cancel', variant: 'ghost' },
            {
                labelKey: 'common.delete',
                variant: 'danger',
                onClick: async () => {
                    try {
                        await apiFetch('/admin/api/settings/prune_expired', { method: 'POST' })
                        ok('settings.danger.prune_done')
                    } catch (err) {
                        fail(err)
                    }
                },
            },
        ],
    })
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

renderPage({
    pageHeader: {
        titleKey: 'settings.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'settings.title' }],
    },
    main: [
        { id: 'username', title: 'settings.username.title', content: { type: 'raw', node: usernameForm() } },
        { id: 'password', title: 'settings.password.title', content: { type: 'raw', node: passwordForm() } },
        { id: 'danger',   title: 'settings.danger.title',   content: { type: 'raw', node: dangerZone() } },
    ],
})
