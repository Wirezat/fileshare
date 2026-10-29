/* fileshare / admin/settings.js — admin credentials, office integration, upload
   webhook, download limit and destructive upkeep. Field markup is built from wui's form classes.
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

function passwordInput(name, value = '') {
    return `<div class="input-wrap">`
        + `<input class="input has-end-icon" type="password" name="${esc(name)}" value="${esc(value)}" autocomplete="off" data-pw>`
        + `<button type="button" class="pw-reveal" tabindex="-1" data-reveal`
        + ` aria-label="${esc(t('settings.reveal'))}">\u{1F441}</button>`
        + `</div>`
}

/* Wires every reveal button in a form. The input is focused afterwards. */
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

/* ── Office integration ──────────────────────────────────────────────────── */

async function loadOffice() {
    try {
        return await (await apiFetch('/admin/api/settings/office')).json()
    } catch {
        return { office_url: '', office_secret: '' }
    }
}

function officeForm(state) {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    const urlField = field('settings.office.url',
        `<input class="input" name="office_url" type="url" autocomplete="off"`
        + ` placeholder="https://office.example.com" value="${esc(state.office_url)}">`)

    const secretField = field('settings.office.secret', passwordInput('office_secret', state.office_secret))

    async function patch(body) {
        await apiFetch('/admin/api/settings/office', {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
    }

    async function save() {
        const url = get('office_url').value.trim()
        const secret = get('office_secret').value.trim()
        if (!url) return showToast({ tone: 'danger', messageKey: t('settings.office.url_required') })
        if (!secret) return showToast({ tone: 'danger', messageKey: t('settings.office.secret_required') })
        try {
            await patch({ office_url: url, office_secret: secret })
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
                            get('office_url').value = ''
                            get('office_secret').value = ''
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

    wireReveals(node)
    return node
}

/* ── Bandwidth ───────────────────────────────────────────────────────────── */

async function loadDownloadLimit() {
    try {
        return await (await apiFetch('/admin/api/settings/download_limit')).json()
    } catch {
        return null
    }
}

function bandwidthForm(state) {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    if (!state) {
        const alert = document.createElement('div')
        alert.className = 'alert danger'
        alert.innerHTML = `<span class="alert-icon">⚠</span><span>${esc(t('settings.bandwidth.load_failed'))}</span>`
        node.append(description('settings.bandwidth.desc'), alert)
        return node
    }

    const group = document.createElement('div')
    group.className = 'radio-group'
    const options = [false, true].map(value => {
        const btn = document.createElement('button')
        btn.type = 'button'
        btn.className = 'radio-opt'
        btn.textContent = t(value ? 'settings.bandwidth.on' : 'settings.bandwidth.off')
        btn.addEventListener('click', () => { enabled = value; paint() })
        group.appendChild(btn)
        return { value, btn }
    })
    const switchField = field('settings.bandwidth.limit', '')
    switchField.appendChild(group)

    const rateField = field('settings.bandwidth.mbit',
        `<input class="input" name="mbit" type="number" min="1" max="100000" step="1"`
        + ` value="${state.mbit || ''}">`, 'settings.bandwidth.mbit_hint')
    const rate = rateField.querySelector('input')

    let enabled = state.enabled
    const paint = () => {
        for (const { value, btn } of options) btn.classList.toggle('active', value === enabled)
        rate.disabled = !enabled
    }

    async function save() {
        const body = { enabled }
        const mbit = Number(rate.value)
        const valid = rate.value !== '' && Number.isInteger(mbit) && mbit >= 1 && mbit <= 100000
        if (valid) {
            body.mbit = mbit
        } else if (enabled) {
            return showToast({ tone: 'danger', messageKey: t('settings.bandwidth.invalid') })
        }
        try {
            await apiFetch('/admin/api/settings/download_limit', {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            })
            ok('settings.bandwidth.done')
        } catch (err) {
            fail(err)
        }
        const saved = await loadDownloadLimit()
        if (saved) {
            enabled = saved.enabled
            rate.value = saved.mbit || ''
            paint()
        }
    }

    node.append(
        description('settings.bandwidth.desc'),
        switchField,
        rateField,
        actions({ labelKey: 'settings.bandwidth.submit', onClick: save }),
    )
    paint()
    return node
}

/* ── Upload webhook ──────────────────────────────────────────────────────── */

const DEFAULT_NTFY = 'https://ntfy.sh'

async function loadWebhook() {
    try {
        return await (await apiFetch('/admin/api/settings/webhook')).json()
    } catch {
        return { webhook_server: '', webhook_topic: '', webhook_token: '' }
    }
}

function randomTopic() {
    const chars = 'abcdefghijklmnopqrstuvwxyz0123456789'
    const bytes = crypto.getRandomValues(new Uint8Array(16))
    return 'fileshare-' + Array.from(bytes, b => chars[b % chars.length]).join('')
}

function webhookForm(state) {
    const node = document.createElement('div')
    node.className = 'form-stack settings-form'
    const get = name => node.querySelector(`[name="${name}"]`)

    async function save() {
        const topic = get('webhook_topic').value.trim()
        try {
            await apiFetch('/admin/api/settings/webhook', {
                method: 'PATCH',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    webhook_server: get('webhook_server').value.trim() || DEFAULT_NTFY,
                    webhook_topic: topic,
                    webhook_token: get('webhook_token').value.trim(),
                }),
            })
            ok(topic ? 'settings.webhook.done' : 'settings.webhook.removed')
        } catch (err) {
            fail(err)
        }
    }

    node.append(
        description('settings.webhook.desc'),
        field('settings.webhook.server',
            `<input class="input" name="webhook_server" type="url" autocomplete="off"`
            + ` placeholder="${DEFAULT_NTFY}" value="${esc(state.webhook_server || DEFAULT_NTFY)}">`),
        field('settings.webhook.topic',
            `<input class="input" name="webhook_topic" type="text" autocomplete="off"`
            + ` value="${esc(state.webhook_topic)}">`,
            'settings.webhook.topic_hint'),
        field('settings.webhook.token', passwordInput('webhook_token', state.webhook_token),
            'settings.webhook.token_hint'),
        actions(
            { labelKey: 'settings.webhook.submit', onClick: save },
            { labelKey: 'settings.webhook.random', variant: 'ghost', onClick: () => { get('webhook_topic').value = randomTopic() } },
        ),
    )
    wireReveals(node)
    return node
}

/* ── Boot ────────────────────────────────────────────────────────────────── */

await bootChrome()

const office = await loadOffice()
const downloadLimit = await loadDownloadLimit()
const webhook = await loadWebhook()

renderPage({
    pageHeader: {
        titleKey: 'settings.title',
        breadcrumb: [{ labelKey: 'admin.crumb', href: PATHS.shares }, { labelKey: 'settings.title' }],
    },
    main: [
        { id: 'username', title: 'settings.username.title', content: { type: 'raw', node: usernameForm() } },
        { id: 'password', title: 'settings.password.title', content: { type: 'raw', node: passwordForm() } },
        { id: 'office',   title: 'settings.office.title',   content: { type: 'raw', node: officeForm(office) } },
        { id: 'webhook',  title: 'settings.webhook.title',  content: { type: 'raw', node: webhookForm(webhook) } },
        { id: 'bandwidth', title: 'settings.bandwidth.title', content: { type: 'raw', node: bandwidthForm(downloadLimit) } },
        { id: 'danger',   title: 'settings.danger.title',   content: { type: 'raw', node: dangerZone() } },
    ],
})
