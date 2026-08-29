import { describe, it, expect } from 'vitest'
import { renderMarkdown } from './markdown.js'

describe('renderMarkdown', () => {
  it('renders basic markdown', () => {
    const h = renderMarkdown('some **bold** and `code`')
    expect(h).toContain('<strong>bold</strong>')
    expect(h).toContain('<code>code</code>')
  })
  it('demotes h1/h2 to plain paragraphs, content kept', () => {
    for (const src of ['# Big Title', '## Sub Title', 'Big Title\n===', '<h1>Big Title</h1>', '<h2>Sub Title</h2>']) {
      const h = renderMarkdown(src)
      expect(h).not.toMatch(/<h[12][\s>]/)
      expect(h).toMatch(/Big Title|Sub Title/)
    }
  })
  it('keeps inline markdown inside demoted headings', () => {
    expect(renderMarkdown('# a **b**')).toContain('<strong>b</strong>')
  })
  it('allows h3–h6', () => {
    expect(renderMarkdown('### ok')).toContain('<h3')
  })
  it('keeps single newlines as line breaks (breaks: true)', () => {
    expect(renderMarkdown('line one\nline two')).toContain('<br')
  })
  it('renders GFM: strikethrough, tables, task lists, autolinks', () => {
    expect(renderMarkdown('~~gone~~')).toContain('<del>')
    expect(renderMarkdown('| a | b |\n|---|---|\n| 1 | 2 |')).toContain('<table')
  })
  it('strips XSS vectors', () => {
    expect(renderMarkdown('<script>alert(1)</script>')).not.toContain('<script')
    expect(renderMarkdown('<img src=x onerror=alert(1)>')).not.toContain('onerror')
    expect(renderMarkdown('[x](javascript:alert(1))')).not.toContain('javascript:')
    expect(renderMarkdown('<iframe src="https://x"></iframe>')).not.toContain('<iframe')
  })
  it('forces links to open in a new tab with rel protection', () => {
    const h = renderMarkdown('[x](https://example.com)')
    expect(h).toContain('target="_blank"')
    expect(h).toContain('rel="noopener noreferrer"')
  })
  it('tolerates empty/nullish input', () => {
    expect(renderMarkdown('')).toBe('')
    expect(renderMarkdown(null)).toBe('')
  })
})

// Adversarial gaps checked during self-review, not called out explicitly by
// the brief's test list: uppercase/nested raw heading tags, the synchronous
// return contract, and the link-hardening hook surviving repeated calls.
describe('renderMarkdown adversarial hardening', () => {
  it('demotes uppercase raw <H1>/<H2> HTML the same as lowercase', () => {
    for (const src of ['<H1>Big Title</H1>', '<H2>Sub Title</H2>']) {
      const h = renderMarkdown(src)
      expect(h.toLowerCase()).not.toMatch(/<h[12][\s>]/)
      expect(h).toMatch(/Big Title|Sub Title/)
    }
  })
  it('keeps nested inline content when demoting a raw heading tag', () => {
    const h = renderMarkdown('<h1><em>Foo</em> bar</h1>')
    expect(h).not.toMatch(/<h1[\s>]/)
    expect(h).toContain('<em>Foo</em>')
    expect(h).toContain('bar')
  })
  it('demotes a raw heading nested inside another element', () => {
    const h = renderMarkdown('<div><h1>Foo</h1></div>')
    expect(h).not.toMatch(/<h1[\s>]/)
    expect(h).toMatch(/Foo/)
  })
  it('always returns a string synchronously, never a Promise', () => {
    const h = renderMarkdown('some **text**')
    expect(typeof h).toBe('string')
    expect(h).not.toBeInstanceOf(Promise)
  })
  it('applies the link target/rel hook on every call, not just the first', () => {
    renderMarkdown('warm up the module')
    const h = renderMarkdown('[x](https://example.com)')
    expect(h).toContain('target="_blank"')
    expect(h).toContain('rel="noopener noreferrer"')
  })
})

// Regression coverage from code review: under happy-dom, DOMPurify's own
// tree-walk (a NodeIterator) has no equivalent of the DOM's "pre-removal
// steps", so when a node gets removed mid-walk the iterator stops dead and
// everything sequenced after that node is emitted unsanitized. jsdom (now
// the configured test environment) implements pre-removal steps correctly,
// so the walk continues past a removal and these payloads get fully
// sanitized. Kept as permanent regressions in case the environment ever
// regresses to something with the same gap.
describe('renderMarkdown — sanitizes content sequenced after a removed node', () => {
  it('strips onerror from an <img> that follows a demoted <h1>', () => {
    const h = renderMarkdown('<h1>x</h1>\n<img src=y onerror=alert(1)>')
    expect(h).not.toContain('onerror')
  })
  it('strips a javascript: href nested inside a demoted <h2>', () => {
    const h = renderMarkdown('<h2><a href="javascript:alert(3)">z</a></h2>')
    expect(h).not.toContain('javascript:')
  })
})

// #id post references: linkified inline so they become in-app navigation
// (PostBody's click delegate), never outbound links. The sanitizer must treat
// a data-postref anchor as in-app NO MATTER where it came from — including a
// crafted raw-HTML anchor pointing somewhere else.
describe('post refs', () => {
  it('linkifies #123 with a data-postref anchor and no target=_blank', () => {
    const html = renderMarkdown('see #123 for context')
    expect(html).toContain('data-postref="123"')
    expect(html).toContain('>#123</a>')
    expect(html).not.toMatch(/data-postref="123"[^>]*target=/)
  })
  it('leaves #123 alone inside code spans and fences', () => {
    expect(renderMarkdown('`#123`')).not.toContain('data-postref')
    expect(renderMarkdown('```\n#123\n```')).not.toContain('data-postref')
  })
  it('does not linkify handles', () => {
    expect(renderMarkdown('@crimson-otter-7')).not.toContain('data-postref')
    expect(renderMarkdown('@123')).not.toContain('data-postref')
  })
  it('still renders a real ATX heading (# needs a space) rather than a ref', () => {
    // #123 is not a heading; `# 123` is — and h1 is demoted to a paragraph.
    expect(renderMarkdown('# 123')).not.toContain('data-postref')
  })
  it('neutralizes a crafted data-postref anchor smuggled as raw HTML', () => {
    const html = renderMarkdown('<a data-postref="1" href="https://evil.example">x</a>')
    expect(html).not.toContain('https://evil.example')
  })
  it('still forces _blank+noopener on real external links', () => {
    const html = renderMarkdown('[x](https://example.com)')
    expect(html).toContain('target="_blank"')
    expect(html).toContain('rel="noopener noreferrer"')
  })

  // Adversarial extras beyond the brief's list: the href must be forced to
  // '#' (not merely stripped of target), a smuggled javascript: href on a
  // postref anchor must not survive either, refs glued to other tokens must
  // not match, and — from code review — the anchor hardening must reach SVG
  // anchors, whose tagName is lowercase 'a' and whose href lives in
  // xlink:href, plus uppercase attribute spellings.
  it('forces a ref anchor href to "#" and strips rel', () => {
    const html = renderMarkdown('see #123')
    expect(html).toMatch(/<a [^>]*href="#"[^>]*data-postref="123"|<a [^>]*data-postref="123"[^>]*href="#"/)
    expect(html).not.toMatch(/data-postref="123"[^>]*rel=/)
  })
  it('neutralizes a javascript: href on a crafted ref anchor', () => {
    const html = renderMarkdown('<a data-postref="1" href="javascript:alert(1)">x</a>')
    expect(html).not.toContain('javascript:')
  })
  it('does not linkify a #id glued to a word or a longer token', () => {
    expect(renderMarkdown('issue#123')).not.toContain('data-postref')
    expect(renderMarkdown('#123abc')).not.toContain('data-postref')
    expect(renderMarkdown('##123')).not.toContain('data-postref')
  })
  it('linkifies several refs in one body', () => {
    const html = renderMarkdown('#1 and #22 both')
    expect(html).toContain('data-postref="1"')
    expect(html).toContain('data-postref="22"')
  })

  // URL fragments are the sigil switch's headline false positive. GFM
  // autolinking claims the whole URL before the extension runs, so the ref
  // tokenizer never fires inside one — the URL must come out as ONE intact
  // external link with no postref anchor nested in it (the HTML parser would
  // otherwise split the anchors apart and the link text would break up).
  it('leaves a URL fragment alone: one intact external link, no ref', () => {
    for (const src of ['https://x.com/a#12', 'https://x.com/#12', 'see https://x.com/a#12 now']) {
      const html = renderMarkdown(src)
      expect(html).not.toContain('data-postref')
      expect(html).toContain('href="https://x.com/')
      // Exactly one anchor: no nested/sibling ref anchor sneaked in.
      expect(html.match(/<a /g)).toHaveLength(1)
    }
  })
  it('leaves an un-autolinked URL fragment alone via the preceding-char guard', () => {
    // No scheme and no `www.`, so GFM autolinking does not claim it; the
    // '/' before the '#' is what keeps it from becoming a ref.
    expect(renderMarkdown('x.com/a#12')).not.toContain('data-postref')
  })
  it('leaves a numeric HTML entity alone', () => {
    expect(renderMarkdown('&#123;')).not.toContain('data-postref')
    expect(renderMarkdown('&#123;')).toContain('{')
  })

  // SVG anchors: node.tagName is the lowercase 'a' (not 'A'), so a hook keyed
  // on tagName skips them entirely — and their href is xlink:href, which the
  // href forcing never touches. Both payloads below kept a live foreign link
  // before the hook was made namespace-agnostic.
  it('neutralizes an svg ref anchor carrying xlink:href', () => {
    const html = renderMarkdown('<svg><a data-postref="1" xlink:href="https://evil.example">x</a></svg>')
    expect(html).not.toContain('evil.example')
  })
  it('neutralizes xlink:href on a plain svg anchor', () => {
    const html = renderMarkdown('<svg><a xlink:href="https://evil.example">x</a></svg>')
    expect(html).not.toContain('evil.example')
  })
  it('neutralizes a crafted ref anchor written with uppercase attribute names', () => {
    const html = renderMarkdown('<a DATA-POSTREF="1" HREF="https://evil.example">x</a>')
    expect(html).not.toContain('evil.example')
    expect(html).toContain('href="#"')
  })
  it('keeps a #id inside link text from nesting an anchor in an anchor', () => {
    const html = renderMarkdown('[see #123](https://example.com)')
    // The HTML parser cannot nest anchors, so the ref anchor ends up as a
    // sibling: the external link's own text must not carry the ref.
    expect(html).toContain('data-postref="123"')
    expect(html).toContain('href="https://example.com"')
    expect(html).not.toMatch(/href="https:\/\/example\.com"[^>]*>[^<]*#123/)
  })
})

// FORBID_TAGS coverage: each named tag must actually disappear, and — where
// the tag can carry meaningful text content — that content must survive
// (DOMPurify's KEEP_CONTENT default), matching how h1/h2 demotion behaves.
describe('renderMarkdown — FORBID_TAGS entries are actually forbidden', () => {
  it('strips <style> and its content entirely (DOMPurify never leaks raw CSS as text)', () => {
    const h = renderMarkdown('<style>body{color:red}</style>')
    expect(h).not.toContain('<style')
    expect(h).not.toContain('body{color:red}')
  })
  it('strips <form>, keeping its child content', () => {
    const h = renderMarkdown('<form><p>hi</p></form>')
    expect(h).not.toContain('<form')
    expect(h).toContain('hi')
  })
  it('strips <button>, keeping its text content', () => {
    const h = renderMarkdown('<button>click</button>')
    expect(h).not.toContain('<button')
    expect(h).toContain('click')
  })
  it('strips <input> entirely (void element, nothing to keep)', () => {
    const h = renderMarkdown('<input value="x">')
    expect(h).not.toContain('<input')
  })
})
