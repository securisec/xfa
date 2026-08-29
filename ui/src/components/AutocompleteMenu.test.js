import { describe, it, expect } from 'vitest'
import { createApp, h, reactive, nextTick } from 'vue'
import AutocompleteMenu from './AutocompleteMenu.vue'

// Detached-app mounting, matching SessionBadge.test.js / PostBody.test.js.
function mount(props, handlers = {}) {
  const el = document.createElement('div')
  const state = reactive({ ...props })
  createApp({ render: () => h(AutocompleteMenu, { ...state, ...handlers }) }).mount(el)
  return { el, state }
}

const ITEMS = [
  { kind: 'post', label: '#42', insert: '#42 ', detail: 'the root of this thread' },
  { kind: 'handle', label: '@crimson-otter-7', insert: '@crimson-otter-7 ', detail: 'handle' },
]

describe('AutocompleteMenu', () => {
  it('renders nothing when closed', () => {
    const { el } = mount({ open: false, items: ITEMS, active: 0 })
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('renders nothing when open with no items', () => {
    const { el } = mount({ open: true, items: [], active: 0 })
    expect(el.querySelector('.acmenu')).toBeFalsy()
  })

  it('renders one row per item, label and detail as plain text', () => {
    const { el } = mount({ open: true, items: ITEMS, active: 0 })
    const rows = el.querySelectorAll('.acrow')
    expect(rows.length).toBe(2)
    expect(rows[0].textContent).toContain('#42')
    expect(rows[0].textContent).toContain('the root of this thread')
    expect(rows[1].textContent).toContain('@crimson-otter-7')
  })

  it('renders a markup-shaped label as text, never as markup', () => {
    const { el } = mount({
      open: true, active: 0,
      items: [{ kind: 'post', label: '#1', insert: '#1 ', detail: '<img src=x onerror=alert(1)>' }],
    })
    const row = el.querySelector('.acrow')
    expect(row.querySelector('img')).toBeFalsy()
    expect(row.textContent).toContain('<img src=x onerror=alert(1)>')
  })

  it('marks the active row and moves the mark when active changes', async () => {
    const { el, state } = mount({ open: true, items: ITEMS, active: 0 })
    let rows = el.querySelectorAll('.acrow')
    expect(rows[0].classList.contains('acrow-on')).toBe(true)
    expect(rows[1].classList.contains('acrow-on')).toBe(false)
    expect(rows[0].getAttribute('aria-selected')).toBe('true')
    state.active = 1
    await nextTick()
    rows = el.querySelectorAll('.acrow')
    expect(rows[0].classList.contains('acrow-on')).toBe(false)
    expect(rows[1].classList.contains('acrow-on')).toBe(true)
  })

  it('is announced as a listbox of options', () => {
    const { el } = mount({ open: true, items: ITEMS, active: 0 })
    expect(el.querySelector('.acmenu').getAttribute('role')).toBe('listbox')
    expect(el.querySelector('.acrow').getAttribute('role')).toBe('option')
  })

  it('emits pick with the row index on mousedown, and suppresses the blur', () => {
    const picked = []
    const { el } = mount(
      { open: true, items: ITEMS, active: 0 },
      { onPick: (i) => picked.push(i) },
    )
    const ev = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    el.querySelectorAll('.acrow')[1].dispatchEvent(ev)
    expect(picked).toEqual([1])
    // Without preventDefault the textarea blurs before the click lands and the
    // menu closes out from under the pick.
    expect(ev.defaultPrevented).toBe(true)
  })

  it('emits hover with the row index on mouseenter', () => {
    const hovered = []
    const { el } = mount(
      { open: true, items: ITEMS, active: 0 },
      { onHover: (i) => hovered.push(i) },
    )
    el.querySelectorAll('.acrow')[1].dispatchEvent(new MouseEvent('mouseenter', { bubbles: true }))
    expect(hovered).toEqual([1])
  })
})
