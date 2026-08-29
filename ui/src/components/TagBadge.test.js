import { describe, it, expect } from 'vitest'
import { createApp, h } from 'vue'
import TagBadge from './TagBadge.vue'
import { tagStyle } from '../lib/format.js'

// Dependency-light mounting, matching PostBody.test.js: a real app rendered
// into a detached element, not @vue/test-utils.
function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(TagBadge, props) }).mount(el)
  return el
}

describe('TagBadge', () => {
  it('renders nothing for an empty tag', () => {
    const el = mount({ tag: '' })
    expect(el.querySelector('.tagpill')).toBeFalsy()
    // v-if leaves only Vue's dev-mode source comment and its own v-if
    // placeholder comment behind — no element at all.
    expect(el.children.length).toBe(0)
  })

  it('renders nothing when tag is undefined (prop omitted)', () => {
    const el = mount({})
    expect(el.querySelector('.tagpill')).toBeFalsy()
    expect(el.children.length).toBe(0)
  })

  it('renders the pill with tagStyle colors for a known tag', () => {
    const el = mount({ tag: 'question' })
    const pill = el.querySelector('.tagpill')
    expect(pill).toBeTruthy()
    // Compare through the DOM's own CSS parsing (jsdom normalizes hex to
    // rgb()/rgba()) rather than a raw string match against the style attr,
    // which would be brittle to that normalization.
    const probe = document.createElement('span')
    probe.style.cssText = tagStyle('question')
    expect(pill.style.color).toBe(probe.style.color)
    expect(pill.style.backgroundColor).toBe(probe.style.backgroundColor)
    expect(pill.textContent).toBe('[question]')
  })

  it('interpolates the tag as text, never as markup', () => {
    const el = mount({ tag: '<b>x</b>' })
    const pill = el.querySelector('.tagpill')
    expect(pill).toBeTruthy()
    expect(pill.querySelector('b')).toBeFalsy()
    expect(pill.textContent).toBe('[<b>x</b>]')
  })
})
