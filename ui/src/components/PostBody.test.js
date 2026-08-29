import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import PostBody from './PostBody.vue'
import { useStore } from '../store.js'
import { runes, BODY_CLIP } from '../lib/format.js'

// Dependency-light mounting: no @vue/test-utils, just a real app rendered into
// a detached element against jsdom — the same DOM the component ships to.
function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(PostBody, props) }).mount(el)
  return el
}

function fire(node, type) {
  node.dispatchEvent(new window.MouseEvent(type, { bubbles: true, cancelable: true }))
}

// The markdown container inside a non-preview body, so assertions about the
// rendered text are not polluted by the expander button's own label.
function md(el) {
  return el.querySelector('.body-md') || el
}

const S = useStore()

beforeEach(() => {
  S.expanded = {}
  S.session = ''
})

describe('PostBody', () => {
  it('renders markdown bodies', () => {
    const el = mount({ post: { id: 1, body: '**hi**' } })
    expect(el.innerHTML).toContain('<strong>hi</strong>')
  })

  it('never renders markdown for tombstones', () => {
    const el = mount({ post: { id: 1, body: '**hi**', deleted: true } })
    expect(el.querySelector('.tomb')).toBeTruthy()
    expect(el.querySelector('.tomb').textContent).toBe('[deleted]')
    expect(el.innerHTML).not.toContain('<strong>')
  })

  it('never renders markdown for tombstones in preview mode either', () => {
    const el = mount({ post: { id: 1, body: '**hi**', deleted: true }, preview: true })
    expect(el.querySelector('.tomb')).toBeTruthy()
    expect(el.innerHTML).not.toContain('<strong>')
    expect(el.querySelector('button.expander')).toBeFalsy()
  })

  it('clips long bodies behind an expander button', () => {
    const el = mount({ post: { id: 1, body: 'x'.repeat(600) } })
    expect(el.querySelector('button.expander')).toBeTruthy()
    expect(runes(md(el).textContent.trim())).toBe(BODY_CLIP)
  })

  it('leaves short bodies alone with no expander', () => {
    const el = mount({ post: { id: 1, body: 'short' } })
    expect(el.querySelector('button.expander')).toBeFalsy()
    expect(md(el).textContent).toContain('short')
  })

  it('cuts on a rune boundary, never mid surrogate pair', () => {
    const el = mount({ post: { id: 1, body: '\u{1F600}'.repeat(600) } })
    const text = md(el).textContent.trim()
    expect(runes(text)).toBe(BODY_CLIP)
    expect(text).not.toContain('�')
    // A UTF-16 slice would have produced 500 code units = 250 emoji + a half.
    expect(text.length).toBe(BODY_CLIP * 2)
  })

  it('labels the collapsed expander with the overflow count', () => {
    const el = mount({ post: { id: 1, body: 'x'.repeat(600) } })
    expect(el.querySelector('button.expander').textContent).toBe('▾ +100 chars')
  })

  it('toggles to the full body and a fold-up label on click', async () => {
    const el = mount({ post: { id: 7, body: 'x'.repeat(600) } })
    fire(el.querySelector('button.expander'), 'click')
    await nextTick()
    expect(el.querySelector('button.expander').textContent).toBe('▴')
    expect(runes(md(el).textContent.trim())).toBe(600)
  })

  it('toggles on dblclick of the body', async () => {
    const el = mount({ post: { id: 8, body: 'x'.repeat(600) } })
    fire(el.querySelector('.post-body'), 'dblclick')
    await nextTick()
    expect(S.expanded[8]).toBe(true)
    fire(el.querySelector('.post-body'), 'dblclick')
    await nextTick()
    expect(S.expanded[8]).toBeFalsy()
  })

  it('ignores dblclick that lands on a control', async () => {
    const el = mount({ post: { id: 9, body: 'x'.repeat(600) } })
    fire(el.querySelector('button.expander'), 'dblclick')
    await nextTick()
    expect(S.expanded[9]).toBeFalsy()
  })

  it('ignores dblclick on a body that does not overflow', async () => {
    const el = mount({ post: { id: 10, body: 'short' } })
    fire(el.querySelector('.post-body'), 'dblclick')
    await nextTick()
    expect(S.expanded[10]).toBeFalsy()
  })

  it('does not clip in preview mode', () => {
    const el = mount({ post: { id: 1, body: 'x'.repeat(600) }, preview: true })
    expect(el.querySelector('button.expander')).toBeFalsy()
  })

  it('renders full markdown inside .clamp3 in preview mode', () => {
    const el = mount({ post: { id: 1, body: '**hi** ' + 'x'.repeat(600) }, preview: true })
    const clamp = el.querySelector('.clamp3')
    expect(clamp).toBeTruthy()
    expect(clamp.innerHTML).toContain('<strong>hi</strong>')
    expect(runes(clamp.textContent.trim())).toBe(603)
  })

  it('does not glue lines together across a <br> in preview mode', () => {
    // marked's breaks:true turns a newline into a bare <br> with no
    // surrounding whitespace text node, so el.textContent for "line
    // one\nline two" is ALWAYS the literal string "line oneline two" here —
    // <br> contributes zero characters to textContent regardless of its CSS,
    // in every DOM engine, not just jsdom (confirmed against marked's raw
    // output: '<p>line one<br>line two</p>\n', no inserted whitespace). The
    // bug this fix addresses is purely visual: with `display: none` the
    // <br> is also dropped from layout, so the rendered line shows no gap
    // between the words either — "oneline". The fix keeps a real,
    // zero-height *inline-block* box there instead, which still occupies
    // horizontal space when the browser lays out the line.
    //
    // So the meaningful regression guard is on the actual CSS rule, not on
    // textContent: pull the live `.clamp3 br` rule out of main.css, apply
    // it for real via a <style> tag, and assert the computed display is not
    // 'none' (i.e. the <br> box survives layout and produces a gap) for the
    // post body actually rendered here. A revert to `display: none` fails
    // this test.
    const css = readFileSync(join(__dirname, '../styles/main.css'), 'utf8')
    const rule = css.match(/\.clamp3 br \{[^}]*\}/)?.[0]
    expect(rule).toBeTruthy()
    expect(rule).not.toContain('display: none')

    const style = document.createElement('style')
    style.textContent = rule
    document.head.appendChild(style)
    try {
      const el = mount({ post: { id: 1, body: 'line one\nline two' }, preview: true })
      const br = el.querySelector('br')
      expect(br).toBeTruthy()
      expect(getComputedStyle(br).display).not.toBe('none')
      expect(el.textContent).toContain('one')
      expect(el.textContent).toContain('two')
    } finally {
      document.head.removeChild(style)
    }
  })

  it('uses the small body class when small', () => {
    const el = mount({ post: { id: 1, body: 'hi' }, small: true })
    expect(el.querySelector('.post-body-sm')).toBeTruthy()
    expect(el.querySelector('.post-body')).toBeFalsy()
  })

  it('uses the normal body class by default', () => {
    const el = mount({ post: { id: 1, body: 'hi' } })
    expect(el.querySelector('.post-body')).toBeTruthy()
    expect(el.querySelector('.post-body-sm')).toBeFalsy()
  })

  it('keeps expansion in the store so it survives a re-render', async () => {
    const first = mount({ post: { id: 42, body: 'x'.repeat(600) } })
    fire(first.querySelector('button.expander'), 'click')
    await nextTick()
    expect(S.expanded[42]).toBe(true)
    // A fresh mount of the same post id (a view switch, or the 5s poll
    // rebuilding the thread) must come back already expanded.
    const second = mount({ post: { id: 42, body: 'x'.repeat(600) } })
    expect(second.querySelector('button.expander').textContent).toBe('▴')
    expect(runes(md(second).textContent.trim())).toBe(600)
  })

  // Parity with the old page, which clipped plain text mid-word: the cut is
  // taken on the SOURCE, so it can land inside a markdown construct. The
  // clipped prefix still renders as valid, sanitized HTML — it just renders
  // as whatever markdown that prefix happens to be.
  it('still produces well-formed HTML when the clip lands inside a code fence', () => {
    const el = mount({ post: { id: 1, body: 'a'.repeat(480) + '\n```js\n' + 'b'.repeat(200) } })
    expect(el.querySelector('button.expander')).toBeTruthy()
    expect(md(el).querySelector('div')).toBeFalsy() // no torn-open wrapper
    expect(md(el).innerHTML).not.toContain('<script')
    expect(md(el).textContent).toContain('aaa')
  })

  it('escapes raw HTML that DOMPurify strips', () => {
    const el = mount({ post: { id: 1, body: '<script>alert(1)</script>ok' } })
    expect(el.innerHTML).not.toContain('<script')
    expect(el.textContent).toContain('ok')
  })

  it('tolerates a body-less post', () => {
    const el = mount({ post: { id: 1 } })
    expect(el.querySelector('button.expander')).toBeFalsy()
    expect(el.querySelector('.post-body')).toBeTruthy()
  })

  // The mount container (`el`) is itself the wrapper the component's root
  // renders into, so a listener on it stands in for a card-level ancestor
  // handler. dblToggleExpand calls stopPropagation() before anything else,
  // so a dblclick dispatched on the markdown content inside .post-body must
  // never reach that ancestor listener — while the toggle itself still runs.
  it('stops a dblclick inside the body from propagating to a parent listener', async () => {
    const el = mount({ post: { id: 11, body: 'x'.repeat(600) } })
    let called = false
    el.addEventListener('dblclick', () => {
      called = true
    })
    fire(el.querySelector('.body-md'), 'dblclick')
    await nextTick()
    expect(called).toBe(false)
    expect(S.expanded[11]).toBe(true)
  })

  // The clip is taken on the markdown SOURCE, so a cut can land inside an
  // open code fence. marked still closes the construct itself (it treats an
  // unterminated fence as running to end of input), so the clipped render
  // must still yield a well-formed <pre><code> pair, not just "no stray
  // markup" (already covered above).
  it('still yields a complete pre/code element when the clip lands inside a code fence', () => {
    const el = mount({ post: { id: 1, body: 'a'.repeat(480) + '\n```js\n' + 'b'.repeat(200) } })
    expect(el.querySelector('button.expander')).toBeTruthy()
    const pre = md(el).querySelector('pre code')
    expect(pre).toBeTruthy()
    expect(pre.textContent).toContain('bbb')
  })

  // Attribute/class fallthrough: PostBody's template has exactly one root
  // element per branch and does not opt out with inheritAttrs: false, so a
  // class passed at the call site should land on that root alongside the
  // component's own 'post-body' class rather than being dropped.
  it('merges a fallthrough class onto the root element alongside post-body', () => {
    const el = mount({ post: { id: 1, body: 'hi' }, class: 'mt-1.5' })
    const root = el.querySelector('.post-body')
    expect(root).toBeTruthy()
    expect(root.classList.contains('mt-1.5')).toBe(true)
  })
})

// #id refs rendered by markdown.js are inert anchors (href="#"); PostBody's
// click delegate is what turns them into in-app navigation. A recorded link
// (the post's OWN links_out, populated only by the thread endpoint) supplies
// the thread root and wins when present; otherwise the bare id is handed to
// the store, which resolves it. Either way the event is swallowed, so the '#'
// href never rewrites the hash route.
describe('PostBody #id ref clicks', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    S.view = 'threads'
    S.threadId = null
    S.thread = []; S.threads = []; S.results = []; S.questions = []; S.inbox = []
  })

  it('navigates to the referenced thread via links_out', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const el = mount({
      post: { id: 9, body: 'see #3', links_out: [{ post_id: 3, thread_id: 3, board_slug: 'b1' }] },
    })
    const a = el.querySelector('a[data-postref]')
    expect(a).toBeTruthy()
    fire(a, 'click')
    await nextTick()
    expect(S.threadId).toBe(3)
    expect(S.view).toBe('thread')
  })

  it('follows a ref whose thread root differs from the referenced post', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const el = mount({
      post: { id: 9, body: 'see #5', links_out: [{ post_id: 5, thread_id: 2, board_slug: 'b1' }] },
    })
    fire(el.querySelector('a[data-postref]'), 'click')
    await nextTick()
    expect(S.threadId).toBe(2)
  })

  // The overwhelming majority of #id refs on a real board have NO post_links
  // row (written before the feature, or under the old sigil), so links_out is
  // empty or missing. The id itself is still globally unique, so the click
  // must resolve it rather than silently doing nothing.
  it('navigates by bare id when the post carries no links_out at all', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const spy = vi.spyOn(S, 'openRef')
    const el = mount({ post: { id: 9, body: 'see #3' }, preview: true })
    fire(el.querySelector('a[data-postref]'), 'click')
    await nextTick()
    expect(spy).toHaveBeenCalledWith(3, null)
    expect(S.threadId).toBe(3)
    expect(S.view).toBe('thread')
  })

  it('navigates by bare id when links_out has no entry for that id', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const spy = vi.spyOn(S, 'openRef')
    const el = mount({
      post: { id: 9, body: 'see #3', links_out: [{ post_id: 4, thread_id: 4, board_slug: 'b1' }] },
    })
    fire(el.querySelector('a[data-postref]'), 'click')
    await nextTick()
    expect(spy).toHaveBeenCalledWith(3, null)
    expect(S.threadId).toBe(3)
  })

  // Always swallowed: an un-navigated ref click must not let the browser act
  // on href="#" (which would clobber the hash route) and must not bubble to a
  // card-level handler.
  it('always preventDefaults and stops propagation, even with no link data', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const el = mount({ post: { id: 9, body: 'see #3' } })
    let bubbled = false
    el.addEventListener('click', () => {
      bubbled = true
    })
    const ev = new window.MouseEvent('click', { bubbles: true, cancelable: true })
    el.querySelector('a[data-postref]').dispatchEvent(ev)
    await nextTick()
    expect(ev.defaultPrevented).toBe(true)
    expect(bubbled).toBe(false)
  })

  it('ignores clicks on ordinary body content', async () => {
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const el = mount({
      post: { id: 9, body: 'plain **text**', links_out: [{ post_id: 3, thread_id: 3, board_slug: 'b1' }] },
    })
    fire(el.querySelector('strong'), 'click')
    await nextTick()
    expect(S.threadId).toBe(null)
  })
})
