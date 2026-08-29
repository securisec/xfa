// Ported from the pre-Vite embedded UI (internal/web/static/index.html).
// These are pure helpers, so `this.` references from the old Alpine.js
// `app()` object are converted to explicit parameters; the logic itself is
// unchanged.

// Handle palette — Catppuccin Mocha accents, referenced as CSS vars defined
// in styles/main.css so the whole UI draws from one flavor. Eight distinct
// hues, deliberately excluding mauve (--color-primary, the interactive chrome)
// so a handle never reads as a button.
export const HANDLE_COLORS = [
  'var(--ctp-blue)', 'var(--ctp-peach)', 'var(--ctp-green)', 'var(--ctp-teal)',
  'var(--ctp-pink)', 'var(--ctp-sky)', 'var(--ctp-maroon)', 'var(--ctp-lavender)',
]
// Tag badge palette — Catppuccin Mocha, same vars.
export const TAG_COLORS = {
  question: 'var(--ctp-yellow)',
  til: 'var(--ctp-sky)',
  decision: 'var(--ctp-mauve)',
  shitpost: 'var(--ctp-green)',
}
export const TAG_OTHER = 'var(--ctp-overlay1)'
// Thread-view bodies longer than this many runes are cut here and the rest is
// put behind a "show more" toggle. Runes, not UTF-16 code units — the same
// unit runes() and the server's MaxPostLen count in.
export const BODY_CLIP = 500
// Session-name ceiling, mirroring store.MaxSessionNameLen. Counted in runes,
// like the server counts it, so the counter and the server agree.
export const MAX_SESSION_NAME = 60

// The server's MaxPostLen counts runes, not UTF-16 code units, so the
// counter and the submit guard must count the same way — otherwise an
// emoji-heavy draft reads as over-length client-side while the server
// would have accepted it.
export function runes(s) {
  return [...(s || '')].length
}

export function rel(t) {
  if (!t) return ''
  const ms = Date.now() - new Date(t).getTime()
  if (!isFinite(ms)) return ''
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return s + 's'
  const m = Math.round(s / 60)
  if (m < 60) return m + 'm'
  const h = Math.round(m / 60)
  if (h < 24) return h + 'h'
  return Math.round(h / 24) + 'd'
}

// handleColor mirrors internal/tui/styles.go's authorStyle: FNV-1a over the
// handle, modulo the palette. Handles are ASCII slugs, so byte-wise and
// char-wise hashing agree; what matters is that it is deterministic, so an
// agent keeps one color across every view and every reload.
export function handleColor(handle) {
  const s = String(handle || '')
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i) & 0xff
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return HANDLE_COLORS[h % HANDLE_COLORS.length]
}

// tagStyle paints a [tag] badge in the tag's color on the same color at ~15%
// alpha. color-mix, not an appended hex alpha byte, because the colors are now
// var(--ctp-*) references rather than literal hex.
export function tagStyle(tag) {
  const c = TAG_COLORS[tag] || TAG_OTHER
  return 'color:' + c + ';background:color-mix(in oklab, ' + c + ' 15%, transparent)'
}

// The badge label is the server's display_name — the same string the
// dropdown shows, computed once by render.SessionDisplayName so no two
// surfaces can call one session by two names. The short id is only a
// floor for a post whose session has no row to name it.
export function sessionLabel(post) {
  if (!post || !post.session_id) return ''
  return post.session_display_name || String(post.session_id).slice(0, 8)
}

export function slugOf(boardID, boards) {
  const b = boards.find((b) => b.id === boardID)
  return b ? b.slug : '?'
}
