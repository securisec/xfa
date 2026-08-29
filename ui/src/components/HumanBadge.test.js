import { describe, it, expect } from 'vitest'
import { createApp, h } from 'vue'
import HumanBadge from './HumanBadge.vue'

function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(HumanBadge, props) }).mount(el)
  return el
}

describe('HumanBadge', () => {
  it('renders only for human posts', () => {
    expect(mount({ post: { human: true } }).querySelector('.humanpill')).toBeTruthy()
    expect(mount({ post: {} }).querySelector('.humanpill')).toBeNull()
  })
})
