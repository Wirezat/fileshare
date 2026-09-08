/* fileshare / admin/chrome.js — shell setup shared by every admin page:
   session config, locale load and sidebar. Nav hrefs must be the real page paths.
*/

import { init }                        from '/static/ui/js/wui.js'
import { configure, logout }           from '/static/ui/js/auth.js'
import { load as loadI18n, getLang, t } from '/static/ui/js/i18n.js'

export const PATHS = {
    shares:   '/admin',
    logs:     '/admin/logs',
    settings: '/admin/settings',
}

/* Configures the session layer, loads the locale and mounts the chrome.
   Resolves before any page code runs, so t() is safe from then on. */
export async function bootChrome() {
    configure({
        mode:       'cookie',
        loginApi:   '/admin/login',
        logoutApi:  '/admin/logout',
        loginPath:  '/admin/login',
        meApi:      '/admin/api/me',
        refreshApi: null,
    })

    // Locale is loaded before init() because the nav labels below call t().
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
        } catch { /* the header clock is not worth a toast */ }
    }
    tick()
    setInterval(tick, 60_000)
}

export function esc(str) {
    return String(str ?? '')
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

/* i18n.js's t() has no interpolation, so placeholders are filled here. The
   result is handed to wui as a "key" that will miss the lookup and come back
   unchanged — the documented fallback behaviour of t(). */
export function tf(key, vars = {}) {
    return Object.entries(vars).reduce(
        (str, [k, v]) => str.replaceAll(`{${k}}`, v), t(key))
}

export async function apiFetch(url, options = {}) {
    const res = await fetch(url, options)
    if (!res.ok) throw new Error((await res.text()).trim() || res.statusText)
    return res
}
