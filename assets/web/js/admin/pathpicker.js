/* fileshare / admin/pathpicker.js — VS Code "Open Folder"-style browsing on a path input. */

import { WuiAutocomplete } from '/static/ui/js/autocomplete.js'
import { apiFetch } from './chrome.js'

const PAGE = 50

function splitPath(value) {
    const cut = value.lastIndexOf('/')
    return { dir: value.slice(0, cut + 1) || '/', filter: value.slice(cut + 1) }
}

function parentOf(dir) {
    const trimmed = dir.slice(0, -1)
    return trimmed.slice(0, trimmed.lastIndexOf('/') + 1) || '/'
}

/* Wraps input, lists one directory level below it and returns { destroy }. */
export function attachPathPicker(input) {
    const wrap = document.createElement('div')
    wrap.className = 'wui-ac-wrap'
    input.replaceWith(wrap)
    wrap.appendChild(input)

    const drop = document.createElement('div')
    drop.className = 'wui-ac-drop'
    document.body.appendChild(drop)

    let listing = { dir: null, entries: [] }
    async function entriesOf(dir) {
        if (listing.dir === dir) return listing.entries
        try {
            const res = await apiFetch(`/admin/api/browse?path=${encodeURIComponent(dir)}`)
            listing = { dir, entries: (await res.json()).entries }
            return listing.entries
        } catch {
            return null
        }
    }

    const descend = path => {
        input.value = path
        input.dispatchEvent(new Event('input'))
        return true
    }

    const ac = new WuiAutocomplete({
        inputEl: input,
        dropEl: drop,
        preselect: false,
        primary: row => row.up ? '..' : row.dir ? `${row.name}/` : row.name,
        icon: row => row.dir ? '📁' : '📄',
        fetch: async (q, offset) => {
            if (!input.isConnected) return { rows: [], hasMore: false }
            const { dir, filter } = splitPath(q)
            const entries = await entriesOf(dir)
            if (!entries) return { rows: [], hasMore: false }
            const f = filter.toLowerCase()
            const rows = entries.filter(e => e.name.toLowerCase().startsWith(f))
            if (dir !== '/' && !filter) rows.unshift({ name: '..', dir: true, up: true })
            return { rows: rows.slice(offset, offset + PAGE), hasMore: offset + PAGE < rows.length }
        },
        onSelect: row => {
            const { dir } = splitPath(input.value)
            if (row.up) return descend(parentOf(dir))
            if (row.dir) return descend(`${dir}${row.name}/`)
            input.value = dir + row.name
        },
    })

    return {
        destroy() {
            ac.destroy()
            drop.remove()
        },
    }
}
