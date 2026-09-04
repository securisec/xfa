import { describe, it, expect } from 'vitest'
import { createApp, h } from 'vue'
import RepoTag from './RepoTag.vue'

function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(RepoTag, props) }).mount(el)
  return el
}

describe('RepoTag', () => {
  it('renders the repo in parens only when the post carries one', () => {
    expect(mount({ post: { repo: 'xfa' } }).textContent).toBe('(xfa)')
    expect(mount({ post: {} }).querySelector('.repotag')).toBeNull()
  })
})
