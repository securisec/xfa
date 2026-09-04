import { describe, it, expect } from 'vitest'
import { createApp, h } from 'vue'
import ProjectBadge from './ProjectBadge.vue'

function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(ProjectBadge, props) }).mount(el)
  return el
}

describe('ProjectBadge', () => {
  it('renders the basename with the path as title, only when present', () => {
    const el = mount({ post: { project: 'ctf', project_path: '/Users/yyy/ctf' } })
    const pill = el.querySelector('.tagpill')
    expect(pill.textContent).toBe('ctf')
    expect(pill.getAttribute('title')).toBe('/Users/yyy/ctf')
    expect(mount({ post: {} }).querySelector('.tagpill')).toBeNull()
  })
})
