/* fileshare admin shell shared by every admin page: session config, locale load and sidebar. */

import { init }                        from '/static/ui/js/wui.js'
import { configure, logout, apiFetch as sessionFetch } from '/static/ui/js/auth.js'
import { load as loadI18n, getLang, t } from '/static/ui/js/i18n.js'

export const PATHS = {
    shares:   '/admin',
    logs:     '/admin/logs',
    settings: '/admin/settings',
}

/* Configures the session layer, loads the locale and mounts the chrome. */
export async function bootChrome() {
    configure({
        mode:       'cookie',
        loginApi:   '/admin/login',
        logoutApi:  '/admin/logout',
        loginPath:  '/admin/login',
        meApi:      '/admin/api/me',
        refreshApi: null,
    })

    await loadI18n(getLang())

    await init({
        nav: [
            { section: t('nav.manage') },
            { href: PATHS.shares, icon: '📋', label: t('nav.shares') },
            { href: PATHS.logs,   icon: '🗒', label: t('nav.logs') },
            { section: t('nav.system') },
            { href: PATHS.settings, icon: '⚙️', label: t('nav.settings') },
        ],
    })

    wireSignOut()
    startUptimeClock()
    startConfigWatch()
}

/* Shows a banner above the page while data.json on disk is not taken over. */
function startConfigWatch() {
    const page = document.getElementById('wui-page-content')
    if (!page) return
    const banner = document.createElement('div')
    banner.className = 'config-banner'
    banner.setAttribute('role', 'alert')
    banner.hidden = true
    page.before(banner)
    const check = async () => {
        try {
            const res = await fetch('/admin/api/me')
            if (!res.ok) return
            const { config_problem: problem } = await res.json()
            banner.hidden = !problem
            banner.textContent = problem ? tf('admin.config_refused', { reason: problem }) : ''
        } catch { }
    }
    check()
    setInterval(check, 15_000)
}

/* Routes the sign-out link through the session layer's logout(). */
function wireSignOut() {
    document.querySelector('[data-wui-logout]')?.addEventListener('click', e => {
        e.preventDefault()
        logout()
    })
}

function startUptimeClock() {
    const el = document.getElementById('uptime-label')
    if (!el) return
    const tick = async () => {
        try {
            const res = await fetch('/admin/api/uptime')
            if (!res.ok) return
            const { uptime_seconds: s } = await res.json()
            const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
            el.textContent = d > 0 ? `↑ ${d}d ${h}h` : h > 0 ? `↑ ${h}h ${m}m` : `↑ ${m}m`
        } catch {}
    }
    tick()
    setInterval(tick, 60_000)
}

export function esc(str) {
    return String(str ?? '')
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

/* t(key) with each {name} in the text replaced by vars[name]; returns the string. */
export function tf(key, vars = {}) {
    return Object.entries(vars).reduce(
        (str, [k, v]) => str.replaceAll(`{${k}}`, v), t(key))
}

/* fetch through wui's session layer; throws when the session is gone and the
   page is already navigating to login.
   @returns {Promise<Response>} */
export async function apiFetch(url, options = {}) {
    const res = await sessionFetch(url, options)
    if (!res) throw new Error(t('admin.session_expired'))
    if (!res.ok) throw new Error((await res.text()).trim() || res.statusText)
    return res
}
