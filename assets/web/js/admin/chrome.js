/* fileshare / admin/chrome.js — shell setup shared by every admin page.

   Each page owns its own body content but not the frame around it, so the
   auth endpoints, the locale load and the sidebar live here once. header.js
   derives the active nav entry from window.location.pathname, which is why
   the hrefs below have to be the real page paths.
*/

import { init }                        from '/static/ui/js/wui.js'
import { configure, logout }           from '/static/ui/js/auth.js'
import { load as loadI18n, getLang, t } from '/static/ui/js/i18n.js'

export const PATHS = {
    shares:   '/admin',
    logs:     '/admin/logs',
    settings: '/admin/settings',
}

/* Resolves before any page code runs, so t() is safe from then on. */
export async function bootChrome() {
    // Same transport the login page configured. Without mode:'cookie' the
    // session layer would look for a Bearer token that does not exist here and
    // read a 401 as "never logged in" instead of "session gone".
    configure({
        mode:       'cookie',
        loginApi:   '/admin/login',
        logoutApi:  '/admin/logout',
        loginPath:  '/admin/login',
        meApi:      '/admin/api/me',
        refreshApi: null,
    })

    // init() loads the locale itself, but every t() in the nav below is
    // evaluated as an argument — before init() ever runs. So the strings have
    // to be there first; the second load inside init() is a no-op fetch.
    await loadI18n(getLang())

    // No `theme` object passed: applyAccentTheme() would write inline styles on
    // <html> that outrank the html.dark rules in theme.css and break the toggle.
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

/* The sign-out control stays an <a href> so it still works without scripting,
   but a click goes through the session layer instead — that is the one place
   that knows how this app's session is carried and how to end it. */
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
