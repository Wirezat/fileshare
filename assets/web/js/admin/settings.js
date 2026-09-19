/* fileshare / admin/settings.js — admin credentials, office integration and
   destructive upkeep. Field markup is built from wui's form classes.
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
    return `<div class="input-wrap">`
        + `<input class="input has-end-icon" type="password" name="${esc(name)}" autocomplete="off" data-pw>`
        + `<button type="button" class="pw-reveal" tabindex="-1" data-reveal`
        + ` aria-label="${esc(t('settings.reveal'))}">\u{1F441}</button>`
        + `</div>`
}

/* Wires every reveal button in a form. The input is focused afterwards, which
   also clears a stored-secret placeholder sitting in it. */
function wireReveals(node) {
    for (const btn of node.querySelectorAll('[data-reveal]')) {
        const input = btn.parentElement.querySelector('input')
        btn.addEventListener('click', () => {
            const hidden = input.type === 'password'
            input.type = hidden ? 'text' : 'password'
            btn.textContent = hidden ? '\u{1F648}' : '\u{1F441}'
            input.focus()
        })
    }
}

function resetReveals(node) {
    for (const btn of node.querySelectorAll('[data-reveal]')) {
        btn.parentElement.querySelector('input').type = 'password'
        btn.textContent = '\u{1F441}'
    }
}

function ok(messageKey) {
    showToast({ messageKey: t(messageKey) })
}

function fail(err) {
    showToast({ tone: 'danger', messageKey: tf('settings.failed', { error: err.message }) })
}

/* Posts a credential change and clears the form on success. */
async function submitCredential(node, url, payload, doneKey) {
    try {
        await apiFetch(url, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        })
        node.querySelectorAll('input').forEach(i => { i.value = '' })
        node.querySelectorAll('.end-icon').forEach(i => { i.textContent = '' })
        resetReveals(node)
        ok(doneKey)
    } catch (err) {
        fail(err)
    }
}

function actions(...buttons) {
    const bar = document.createElement('div')
    bar.className = 'form-actions'
    for (const { labelKey, variant = 'primary', onClick } of buttons) {
        const btn = document.createElement('button')
        btn.type = 'button'
        btn.className = `btn btn-${variant}`
        btn.textContent = t(labelKey)
        btn.addEventListener('click', onClick)
        bar.appendChild(btn)
    }
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
        field('settings.current', passwordInput('current')),
        field('settings.username.new', `<input class="input" name="username" autocomplete="off">`),
        actions({ labelKey: 'settings.username.submit', onClick: () => {
            const current = get('current').value
            const username = get('username').value.trim()
            if (!current) return showToast({ tone: 'danger', messageKey: t('settings.current_required') })
            if (!username) return showToast({ tone: 'danger', messageKey: t('settings.username.empty') })
            submitCredential(node, '/admin/api/settings/username',
                { current_password: current, new_username: username }, 'settings.username.done')
        } }),
    )
    wireReveals(node)
    return node
}

/* ── Password ────────────────────────────────────────────────────────────── */

function passwordForm() {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    const confirmField = field('settings.password.confirm',
        `<div class="input-wrap">`
        + `<input class="input has-end-icon" type="password" name="confirm" autocomplete="off">`
        + `<span class="end-icon" data-match></span></div>`)

    node.append(
        description('settings.password.desc'),
        field('settings.current', passwordInput('current')),
        field('settings.password.new', passwordInput('next')),
        confirmField,
        actions({ labelKey: 'settings.password.submit', onClick: () => {
            const current = get('current').value
            const next = get('next').value
            const confirm = get('confirm').value
            if (!current) return showToast({ tone: 'danger', messageKey: t('settings.current_required') })
            if (!next) return showToast({ tone: 'danger', messageKey: t('settings.password.empty') })
            if (next !== confirm) return showToast({ tone: 'danger', messageKey: t('settings.password.mismatch') })
            submitCredential(node, '/admin/api/settings/password',
                { current_password: current, new_password: next }, 'settings.password.done')
        } }),
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

    wireReveals(node)
    return node
}

/* ── Danger zone ─────────────────────────────────────────────────────────── */

function dangerZone() {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'

    const alert = document.createElement('div')
    alert.className = 'alert danger'
    alert.innerHTML = `<span class="alert-icon">⚠</span><span>${esc(t('settings.danger.prune_warning'))}</span>`

    node.append(alert, actions(
        { labelKey: 'settings.danger.prune', variant: 'danger', onClick: confirmPrune },
    ))
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

const SECRET_PLACEHOLDER = '\u2022'.repeat(24)

/* ── Office integration ──────────────────────────────────────────────────── */

/* Reads the stored connection. The secret itself never leaves the server, so
   the response only says whether one exists. */
async function loadOffice() {
    try {
        return await (await apiFetch('/admin/api/settings/office')).json()
    } catch {
        return { office_url: '', secret_set: false }
    }
}

function officeForm(state) {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    const urlField = field('settings.office.url',
        `<input class="input" name="office_url" type="url" autocomplete="off"`
        + ` placeholder="https://office.example.com" value="${esc(state.office_url)}">`)

    const secretField = field('settings.office.secret', passwordInput('office_secret'))

    /* A stored secret never reaches the browser, so the field shows a
       placeholder of the same shape a saved password has. Focusing clears it
       for typing; leaving it untouched puts it back, so a stray click does not
       read as a deletion. */
    const paint = () => {
        const el = get('office_secret')
        if (state.secret_set) {
            el.value = SECRET_PLACEHOLDER
            el.dataset.stored = '1'
        } else {
            el.value = ''
            delete el.dataset.stored
        }
    }

    const typed = () => (get('office_secret').dataset.stored ? '' : get('office_secret').value)

    async function patch(body) {
        await apiFetch('/admin/api/settings/office', {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
    }

    /* The secret field is left empty to keep the stored one, so it is only sent
       when the admin actually typed a new one. */
    async function save() {
        const url = get('office_url').value.trim()
        const secret = typed()
        if (!url) return showToast({ tone: 'danger', messageKey: t('settings.office.url_required') })
        if (!secret && !state.secret_set) {
            return showToast({ tone: 'danger', messageKey: t('settings.office.secret_required') })
        }
        const body = { office_url: url }
        if (secret) body.office_secret = secret
        try {
            await patch(body)
            state.office_url = url
            if (secret) state.secret_set = true
            paint()
            ok('settings.office.done')
        } catch (err) {
            fail(err)
        }
    }

    function confirmRemove() {
        showModal({
            preset: 'danger-confirm',
            titleKey: 'settings.office.title',
            messageKey: 'settings.office.remove_confirm',
            actions: [
                { labelKey: 'common.cancel', variant: 'ghost' },
                {
                    labelKey: 'common.delete',
                    variant: 'danger',
                    onClick: async () => {
                        try {
                            await patch({ office_url: '', office_secret: '' })
                            state.office_url = ''
                            state.secret_set = false
                            get('office_url').value = ''
                            paint()
                            ok('settings.office.removed')
                        } catch (err) {
                            fail(err)
                        }
                    },
                },
            ],
        })
    }

    node.append(
        description('settings.office.desc'),
        urlField,
        secretField,
        actions(
            { labelKey: 'settings.office.submit', onClick: save },
            { labelKey: 'settings.office.remove', variant: 'danger', onClick: confirmRemove },
        ),
    )

    const secretEl = get('office_secret')
    secretEl.addEventListener('focus', () => {
        if (secretEl.dataset.stored) {
            secretEl.value = ''
            delete secretEl.dataset.stored
        }
    })
    secretEl.addEventListener('blur', () => {
        if (state.secret_set && secretEl.value === '') paint()
    })

    wireReveals(node)
    paint()
    return node
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

const office = await loadOffice()

renderPage({
    pageHeader: {
        titleKey: 'settings.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'settings.title' }],
    },
    main: [
        { id: 'username', title: 'settings.username.title', content: { type: 'raw', node: usernameForm() } },
        { id: 'password', title: 'settings.password.title', content: { type: 'raw', node: passwordForm() } },
        { id: 'office',   title: 'settings.office.title',   content: { type: 'raw', node: officeForm(office) } },
        { id: 'danger',   title: 'settings.danger.title',   content: { type: 'raw', node: dangerZone() } },
    ],
})
