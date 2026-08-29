import { describe, it, expect, beforeEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import ComposerModal from './ComposerModal.vue'
import ThreadDetail from './views/ThreadDetail.vue'
import { useStore } from '../store.js'

// The composers are mounted for real — this suite is about the WIRING (the
// three textareas the human actually types into), so a stand-in harness
// component would test nothing worth testing.
function mount(Comp) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  createApp({ render: () => h(Comp) }).mount(el)
  return el
}

// Typing, as the browser does it: the value and the caret move first, then the
// input event fires.
function type(ta, text, caret) {
  ta.value = text
  const c = caret === undefined ? text.length : caret
  ta.setSelectionRange(c, c)
  ta.dispatchEvent(new Event('input', { bubbles: true }))
}

function press(ta, key) {
  const ev = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
  ta.dispatchEvent(ev)
  return ev
}

const S = useStore()

beforeEach(() => {
  S.boards = [{ id: 1, slug: 'xfa' }]
  S.composer = { open: true, board: 'xfa', body: '', tag: '' }
  S.reply = { body: '' }
  S.inline = { parentId: 0, body: '' }
  S.threadId = 42
  S.thread = [
    { id: 42, author: 'crimson-otter-7', body: 'the root post', created_at: '2026-08-24T00:00:00Z' },
    { id: 43, author: 'azure-lynx-3', parent_id: 42, body: 'a reply', created_at: '2026-08-24T00:01:00Z' },
  ]
  S.threads = []; S.results = []; S.questions = []; S.inbox = []
  S.sessions = []; S.session = ''; S.expanded = {}
})

describe('composer autocomplete wiring', () => {
  it('opens the menu when @ is typed and filters as the query grows', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    expect(el.querySelector('.acmenu')).toBeFalsy()

    type(ta, 'look at @')
    await nextTick()
    const rows = el.querySelectorAll('.acrow')
    expect(rows.length).toBeGreaterThan(1)      // a bare @ offers every handle

    type(ta, 'look at @cri')
    await nextTick()
    const filtered = [...el.querySelectorAll('.acrow')].map((r) => r.textContent)
    expect(filtered.length).toBe(1)
    expect(filtered[0]).toContain('@crimson-otter-7')
  })

  // The sigil picks the namespace: `#` reaches the posts and only the posts,
  // `@` the handles and only the handles. Neither menu ever mixes the two.
  it('opens a post-only menu when # is typed, and a handle-only menu for @', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')

    type(ta, 'see #')
    await nextTick()
    let labels = [...el.querySelectorAll('.acrow')].map((r) => r.textContent)
    expect(labels.length).toBe(2)                 // posts 43 and 42, thread first
    expect(labels.every((t) => t.includes('#') && !t.includes('@'))).toBe(true)

    type(ta, 'see @')
    await nextTick()
    labels = [...el.querySelectorAll('.acrow')].map((r) => r.textContent)
    expect(labels.length).toBe(2)                 // both handles
    expect(labels.every((t) => t.includes('@'))).toBe(true)
  })

  it('offers nothing for @ followed by digits: handles are never bare numbers', async () => {
    const el = mount(ComposerModal)
    type(el.querySelector('textarea'), 'see @43')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('does not open inside a URL fragment', async () => {
    const el = mount(ComposerModal)
    type(el.querySelector('textarea'), 'see https://x.com/a#4')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('lifts the modal box clip only while the menu is open', async () => {
    // .modal-box scrolls its own overflow, which would cut the dropdown off at
    // the bottom of the composer instead of letting it overlap.
    const el = mount(ComposerModal)
    const box = el.querySelector('.modal-box')
    const ta = el.querySelector('textarea')
    expect(box.classList.contains('acmenu-open')).toBe(false)
    type(ta, '@cri')
    await nextTick()
    expect(box.classList.contains('acmenu-open')).toBe(true)
    press(ta, 'Escape')
    await nextTick()
    expect(box.classList.contains('acmenu-open')).toBe(false)
  })

  it('does not open inside an email address', async () => {
    const el = mount(ComposerModal)
    type(el.querySelector('textarea'), 'mail me at alice@cri')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('closes when the query matches nothing', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '@cri')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeTruthy()
    type(ta, '@crizzzz')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('moves the active row with ArrowDown/ArrowUp and wraps', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '@')
    await nextTick()
    const n = el.querySelectorAll('.acrow').length
    expect(n).toBeGreaterThan(1)
    const activeIndex = () => [...el.querySelectorAll('.acrow')].findIndex((r) => r.classList.contains('acrow-on'))
    expect(activeIndex()).toBe(0)

    expect(press(ta, 'ArrowDown').defaultPrevented).toBe(true)
    await nextTick()
    expect(activeIndex()).toBe(1)

    press(ta, 'ArrowUp'); await nextTick()
    expect(activeIndex()).toBe(0)
    press(ta, 'ArrowUp'); await nextTick()
    expect(activeIndex()).toBe(n - 1)          // wraps to the end
    press(ta, 'ArrowDown'); await nextTick()
    expect(activeIndex()).toBe(0)              // and back round
  })

  it('accepts the active row on Enter, updating the bound store field', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, 'see @cri')
    await nextTick()
    const ev = press(ta, 'Enter')
    // Enter must not also insert a newline while the menu is open.
    expect(ev.defaultPrevented).toBe(true)
    await nextTick()
    expect(S.composer.body).toBe('see @crimson-otter-7 ')
    expect(el.querySelector('.acmenu')).toBeFalsy()
    await nextTick()
    expect(ta.value).toBe('see @crimson-otter-7 ')
    expect(ta.selectionStart).toBe('see @crimson-otter-7 '.length)
  })

  it('accepts on Tab too', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '#43')
    await nextTick()
    expect(press(ta, 'Tab').defaultPrevented).toBe(true)
    await nextTick()
    expect(S.composer.body).toBe('#43 ')
  })

  it('leaves Enter alone when no menu is open, so it still inserts a newline', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, 'no token here')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
    expect(press(ta, 'Enter').defaultPrevented).toBe(false)
    expect(press(ta, 'Tab').defaultPrevented).toBe(false)
  })

  it('closes on Escape without also closing the composer', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '@cri')
    await nextTick()
    expect(press(ta, 'Escape').defaultPrevented).toBe(true)
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
    // The modal's own window-level Escape handler must not have fired: the
    // first Escape dismisses the menu, a second one dismisses the modal.
    expect(S.composer.open).toBe(true)
    press(ta, 'Escape')
    await nextTick()
    expect(S.composer.open).toBe(false)
  })

  it('closes when the caret walks away from the token', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '@cri')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeTruthy()
    press(ta, 'ArrowLeft')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('closes on blur', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '@cri')
    await nextTick()
    ta.dispatchEvent(new FocusEvent('blur', { bubbles: false }))
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('accepts a row that is clicked', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, '#4')
    await nextTick()
    const rows = el.querySelectorAll('.acrow')
    expect(rows.length).toBe(2)                   // posts 43 and 42
    rows[1].dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()
    expect(S.composer.body).toBe('#42 ')
  })

  it('completes a mid-text token without eating what follows', async () => {
    const el = mount(ComposerModal)
    const ta = el.querySelector('textarea')
    type(ta, 'ping @cri about it', 9)             // caret sits right after '@cri'
    await nextTick()
    press(ta, 'Enter')
    await nextTick()
    expect(S.composer.body).toBe('ping @crimson-otter-7 about it')
  })

  it('drives the thread view bottom reply composer', async () => {
    const el = mount(ThreadDetail)
    const areas = el.querySelectorAll('textarea')
    expect(areas.length).toBe(1)                  // no inline composer open yet
    const ta = areas[0]
    type(ta, '@azu')
    await nextTick()
    expect(el.querySelectorAll('.acrow').length).toBe(1)
    press(ta, 'Enter')
    await nextTick()
    expect(S.reply.body).toBe('@azure-lynx-3 ')
  })

  it('drives the thread view inline reply composer', async () => {
    S.inline.parentId = 42
    const el = mount(ThreadDetail)
    const areas = el.querySelectorAll('textarea')
    expect(areas.length).toBe(2)                  // inline first, then the bottom one
    const ta = areas[0]
    type(ta, '#43')
    await nextTick()
    press(ta, 'Enter')
    await nextTick()
    expect(S.inline.body).toBe('#43 ')
    expect(S.reply.body).toBe('')                 // the other composer is untouched
  })

  it('drops a dangling menu when the inline composer is torn down without a blur', async () => {
    // A browser fires no blur when an element is REMOVED, and the inline
    // composer is removed out from under an open menu by two ordinary paths:
    // a hashchange (applyRoute → closeInline) and the 5s poll dropping a
    // hard-deleted post's row. A menu left open then greets the next inline
    // composer already open, and its first Enter would be swallowed and
    // splice a stale completion into an empty textarea.
    S.inline.parentId = 42
    const el = mount(ThreadDetail)
    type(el.querySelectorAll('textarea')[0], '@cri')
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeTruthy()

    S.closeInline()                               // what a route change does
    await nextTick()
    S.openInline(43)                              // the human opens another one
    await nextTick()
    expect(el.querySelector('.acmenu')).toBeFalsy()

    const ta = el.querySelectorAll('textarea')[0]
    expect(ta.value).toBe('')
    const ev = press(ta, 'Enter')
    expect(ev.defaultPrevented).toBe(false)       // Enter is the newline's again
    await nextTick()
    expect(S.inline.body).toBe('')                // and nothing was spliced in
  })
})
