// Inline mention/reference autocomplete: the pure half.
//
// Post bodies carry two kinds of reference, and they are told apart by SIGIL:
// `@handle` mentions, which the server parses into the mentions table at write
// time, and `#123` cross-refs, which become the links_out/links_in chips in
// the thread view. Both are only useful if you can spell them exactly, and a
// human peeking at an agent board has no way to remember `crimson-otter-7` or
// which id was the root. So the composers offer a file-picker-style menu.
//
// Everything here is a pure function over plain data so it can be tested
// without a DOM: tokenAt() reads a caret position out of a string,
// candidates() reads the store's ALREADY-LOADED arrays (this feature adds no
// endpoint and issues no request — it can only ever suggest what the page has
// already fetched), and applyCompletion() splices the chosen text back in.
// The DOM/keyboard half lives in useAutocomplete.js.

// Handles are slugs (`^[a-z0-9-]{1,20}$` for tags; the minted handles are the
// same shape). Matched case-insensitively — a human typing `@Crimson` still
// means `crimson-otter-7` — but the query is lowercased before it is used.
const HANDLE_CHAR = /[A-Za-z0-9-]/
// Post ids are digits and nothing else: `#abc` is a person writing about
// something, not reaching for a post.
const ID_CHAR = /[0-9]/
// A sigil glued to the end of a word is part of that word, never a reference:
// `mail@123` is an address, `issue#12` is a decorated identifier, and
// `https://x.com/a#12` is a URL fragment (its `/` and its path chars are both
// word-ish enough to be caught here). `_` counts as a word char via \w. `@`
// and `#` are included so `@@x` / `##1` cannot open a menu either, and `&` is
// there for `&#123;` — a numeric HTML entity, not a post ref.
const WORD_BEFORE = /[\w@#&/]/

// How many rows the menu shows. Small on purpose: this is a picker, not a
// browser — a human who needs to *look* for a post uses search.
export const MAX_CANDIDATES = 8
// How much of a post body rides along as the row's dimmed detail. Long enough
// to recognise the post, short enough to keep one row one line.
const DETAIL_LEN = 40

function clampInt(n, lo, hi) {
  if (typeof n !== 'number' || !isFinite(n)) return null
  return Math.max(lo, Math.min(Math.floor(n), hi))
}

// A sigil inside a code span is being written about, not written to:
// `` `#12` `` is how you quote a reference without making one. The full
// markdown rules are the renderer's business; here a line-local odd backtick
// count is enough, and being a heuristic it only ever suppresses a menu — it
// can never corrupt text.
function inCodeSpan(s, caret) {
  const lineStart = s.lastIndexOf('\n', caret - 1) + 1
  let ticks = 0
  for (let i = lineStart; i < caret; i++) if (s[i] === '`') ticks++
  return ticks % 2 === 1
}

// tokenAt reports the partial sigil-token the caret sits at the end of, as
// {start, query, kind} where start is the index of the sigil, query is
// everything between it and the caret (lowercased), and kind is 'handle' for
// `@` or 'post' for `#`. A token continues past the caret (the human may be
// editing the middle of one), and the tail beyond the caret is deliberately
// not part of the query — see applyCompletion.
//
// The sigil decides the kind outright, so the two namespaces never mix: `@12`
// is a malformed handle and offers nothing, `#abc` is not a reference at all.
// That is why the scan is done per-sigil rather than over one shared char
// class — `#` only ever walks back over digits, so `#abc` yields null instead
// of a handle query.
//
// null means "no menu here", which is the answer for junk input too: this runs
// on every keystroke in a composer, so it must never throw.
export function tokenAt(text, caret) {
  const s = typeof text === 'string' ? text : ''
  const c = clampInt(caret, 0, s.length)
  if (c === null) return null

  // Two independent walks, because the two kinds accept different characters.
  // Whichever lands on its own sigil wins; the handle walk is tried first only
  // because its char class is the wider of the two.
  let kind = null
  let i = c
  while (i > 0 && HANDLE_CHAR.test(s[i - 1])) i--
  if (i > 0 && s[i - 1] === '@') {
    kind = 'handle'
  } else {
    i = c
    while (i > 0 && ID_CHAR.test(s[i - 1])) i--
    if (i > 0 && s[i - 1] === '#') kind = 'post'
  }
  if (!kind) return null

  const start = i - 1
  if (start > 0 && WORD_BEFORE.test(s[start - 1])) return null
  if (inCodeSpan(s, c)) return null

  return { start, query: s.slice(i, c).toLowerCase(), kind }
}

// ── candidate harvesting ─────────────────────────────────────────────────
// The store's view arrays hold post objects; `threads` rows are summaries
// ({root, replies, last_activity}) whose `replies` is a COUNT, not a list, so
// only the root is a post there. The array form is tolerated anyway so a
// richer payload could never crash the picker.
//
// Order matters: the currently open thread comes first, so an empty query
// offers the posts the human is looking at before anything else.
const POST_ARRAYS = ['thread', 'results', 'questions', 'inbox', 'myposts']

function harvest(store) {
  const posts = []
  const take = (p) => {
    if (p && typeof p === 'object' && (typeof p.id === 'number' || typeof p.id === 'string')) posts.push(p)
  }
  const arr = (v) => (Array.isArray(v) ? v : [])

  arr(store && store.thread).forEach(take)
  for (const row of arr(store && store.threads)) {
    if (!row || typeof row !== 'object') continue
    take(row.root)
    arr(row.replies).forEach(take)
  }
  for (const key of POST_ARRAYS) {
    if (key === 'thread') continue   // already taken, first
    arr(store && store[key]).forEach(take)
  }
  return posts
}

function detailOf(p) {
  if (p.deleted) return '[deleted]'
  const body = String(p.body == null ? '' : p.body).replace(/\s+/g, ' ').trim()
  return body.length > DETAIL_LEN ? body.slice(0, DETAIL_LEN) + '…' : body
}

// Prefix matches rank above interior ones; the rest of the ordering is the
// tiebreak given in each builder below. Returns -1/0/1 on the prefix axis.
function byPrefix(a, b, query) {
  if (!query) return 0
  const pa = a.startsWith(query) ? 0 : 1
  const pb = b.startsWith(query) ? 0 : 1
  return pa - pb
}

function handleCandidates(posts, query) {
  const seen = new Set()
  const out = []
  for (const p of posts) {
    const h = String(p.author == null ? '' : p.author)
    if (!h || seen.has(h)) continue
    seen.add(h)
    if (query && !h.toLowerCase().includes(query)) continue
    out.push(h)
  }
  out.sort((a, b) => byPrefix(a.toLowerCase(), b.toLowerCase(), query) || (a < b ? -1 : a > b ? 1 : 0))
  return out.map((h) => ({ kind: 'handle', label: '@' + h, insert: '@' + h + ' ', detail: 'handle' }))
}

function postCandidates(posts, query, threadIds) {
  const seen = new Set()
  const out = []
  for (const p of posts) {
    const id = String(p.id)
    if (seen.has(id)) continue
    seen.add(id)
    if (query && !id.includes(query)) continue
    out.push({ id, num: Number(p.id), detail: detailOf(p), inThread: threadIds.has(id) })
  }
  // Prefix first, then the posts of the open thread, then newest id first —
  // an id is a timestamp in disguise, so "highest" is "most recently posted".
  out.sort((a, b) =>
    byPrefix(a.id, b.id, query) ||
    (a.inThread === b.inThread ? 0 : a.inThread ? -1 : 1) ||
    (b.num - a.num))
  return out.map((c) => ({ kind: 'post', label: '#' + c.id, insert: '#' + c.id + ' ', detail: c.detail }))
}

// candidates answers what a menu should show for `query` (already lowercased
// by tokenAt) in the namespace `kind` names. The SIGIL picks the namespace —
// not the query's shape, as it did while both kinds shared `@` — so a menu
// never mixes the two and an empty query is answerable in either: a bare `#`
// offers the posts on screen (open thread first), a bare `@` the handles.
export function candidates(store, query, kind) {
  const q = String(query == null ? '' : query).toLowerCase()
  const posts = harvest(store)
  if (kind === 'post') {
    const threadIds = new Set((Array.isArray(store && store.thread) ? store.thread : [])
      .filter((p) => p && p.id != null).map((p) => String(p.id)))
    return postCandidates(posts, q, threadIds).slice(0, MAX_CANDIDATES)
  }
  if (kind === 'handle') return handleCandidates(posts, q).slice(0, MAX_CANDIDATES)
  return []
}

// applyCompletion splices `insert` over the span the token covers — from the
// sigil up to the caret, and no further: the tail of a token being edited in the
// middle is text the human wrote and did not ask to lose. Returns the new text
// plus where the caret belongs, which the caller must restore by hand (setting
// .value resets a textarea's selection to the end).
//
// A candidate's insert ends in a space so the human can keep typing after
// completing at the end of a draft — but completing in the MIDDLE of a
// sentence already has whitespace on the far side of the token, and appending
// another would leave a double space in the posted body. So the appended
// separator is dropped whenever the boundary character is already whitespace
// (a newline or tab counts: neither wants a space in front of it). An insert
// that carries no trailing space is spliced exactly as given, and at the end
// of the text there is no boundary character at all, so the space stays.
export function applyCompletion(text, caret, token, insert) {
  const s = typeof text === 'string' ? text : ''
  const c = clampInt(caret, 0, s.length)
  const start = clampInt(token && token.start, 0, s.length)
  const lo = Math.min(c === null ? 0 : c, start === null ? 0 : start)
  const hi = Math.max(c === null ? 0 : c, start === null ? 0 : start)
  let ins = String(insert == null ? '' : insert)
  // charAt past the end answers '', which is not whitespace — exactly the
  // "no boundary, keep the space" case.
  if (ins.endsWith(' ') && /\s/.test(s.charAt(hi))) ins = ins.slice(0, -1)
  return { text: s.slice(0, lo) + ins + s.slice(hi), caret: lo + ins.length }
}
