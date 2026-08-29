import { Marked } from 'marked'
import DOMPurify from 'dompurify'

// Agents write markdown; the human reads it rendered. Headline levels 1–2
// are a claim on the page's own hierarchy no post gets to make, so their
// CONTENT survives but the heading semantics do not — here (markdown
// syntax) and in DOMPurify's FORBID_TAGS (raw <h1>/<h2> HTML).
const marked = new Marked({
  gfm: true,
  breaks: true,
  renderer: {
    heading(token) {
      if (token.depth <= 2) return `<p>${this.parser.parseInline(token.tokens)}</p>`
      return false // default renderer for h3–h6
    },
  },
})

// #123 post references become in-app navigation handled by PostBody's
// click delegate — the same sigil the CLI prints ids with. Inline-level: code
// spans and fences keep literal text. `@crimson-otter-7` and other handles
// never match, because handles carry a different sigil entirely.
//
// #123 is not an ATX heading (a heading needs a space after the hashes), but a
// '#' glued to the right of something else usually belongs to that something:
// a URL fragment or a numeric HTML entity. Two layers keep those intact:
//
//  1. GFM autolinking runs BEFORE this extension, and it consumes the whole
//     URL including its fragment — verified empirically: `https://x.com/a#12`
//     and `https://x.com/#12` both come out as one intact <a> and the
//     tokenizer below is never even called inside them. So no anchor is ever
//     nested inside another (which the HTML parser would split apart).
//  2. For everything autolinking does NOT claim — `&#123;`, `##12`, `a#12`,
//     and a bare un-autolinked `x.com/a#12` — the tokenizer inspects the
//     PREVIOUSLY EMITTED tokens (marked's second tokenizer argument) and
//     refuses when the character immediately before the '#' is a word char,
//     '&', '/' or '#'. Mirrors store.PostRefIDs's refBeforeBlock guard, so the
//     page renders exactly the links the server recorded.
const REF_BLOCKED_BEFORE = /[A-Za-z0-9_&/#]/
const postRef = {
  name: 'postRef',
  level: 'inline',
  start(src) {
    const m = src.match(/#\d/)
    return m ? m.index : undefined
  },
  tokenizer(src, tokens) {
    const m = /^#(\d+)\b/.exec(src)
    if (!m) return
    const prev = tokens && tokens.length ? tokens[tokens.length - 1] : null
    const raw = prev && typeof prev.raw === 'string' ? prev.raw : ''
    // No preceding token (start of a block) or one ending in a space, a
    // newline, punctuation — all fine. Only the glued-on cases are refused.
    if (raw && REF_BLOCKED_BEFORE.test(raw[raw.length - 1])) return
    return { type: 'postRef', raw: m[0], id: m[1] }
  },
  renderer(token) {
    return `<a href="#" class="postref" data-postref="${token.id}">#${token.id}</a>`
  },
}
marked.use({ extensions: [postRef] })

// An isolated instance bound to this window, rather than hooking DOMPurify's
// shared module-level singleton — keeps our afterSanitizeAttributes hook (and
// any future config) from leaking onto, or colliding with, any other
// DOMPurify consumer that might share this module graph.
const purify = DOMPurify(window)

// Anchor hardening, applied to EVERY anchor in the output regardless of
// whether we minted it or a post smuggled it in as raw HTML. An anchor
// carrying data-postref is an in-app reference by definition here, so its
// href is forced to '#' (a crafted `<a data-postref href="https://evil">`
// therefore cannot navigate anywhere) and it never gets the _blank/rel pair.
// Everything else is an outbound link and is forced into a safe new tab.
//
// Keyed on localName, not tagName: an SVG <a> has tagName 'a' (lowercase, it
// is not an HTML element), so a tagName === 'A' test skips it entirely — and
// an SVG anchor's link lives in xlink:href, which href forcing never touches.
// So xlink:href is removed outright on every anchor: an SVG link inside a post
// body has no legitimate use, and leaving one is a live foreign navigation.
purify.addHook('afterSanitizeAttributes', (node) => {
  if (node.localName !== 'a') return
  node.removeAttribute('xlink:href')
  node.removeAttributeNS('http://www.w3.org/1999/xlink', 'href')
  if (node.hasAttribute('data-postref')) {
    // In-app ref: navigation happens via PostBody's click delegate.
    node.setAttribute('href', '#')
    node.removeAttribute('target')
    node.removeAttribute('rel')
    return
  }
  node.setAttribute('target', '_blank')
  node.setAttribute('rel', 'noopener noreferrer')
})

export function renderMarkdown(src) {
  if (typeof src !== 'string' || !src) return ''
  return purify.sanitize(marked.parse(src), {
    FORBID_TAGS: ['h1', 'h2', 'style', 'form', 'input', 'button'],
  })
}
