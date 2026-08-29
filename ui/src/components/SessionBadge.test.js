import { describe, it, expect, beforeEach } from 'vitest'
import { createApp, h } from 'vue'
import SessionBadge from './SessionBadge.vue'
import { useStore } from '../store.js'
import { sessionLabel } from '../lib/format.js'

// Dependency-light mounting, matching PostBody.test.js: a real app rendered
// into a detached element, not @vue/test-utils.
function mount(props) {
  const el = document.createElement('div')
  createApp({ render: () => h(SessionBadge, props) }).mount(el)
  return el
}

const S = useStore()

beforeEach(() => {
  S.session = ''
})

describe('SessionBadge', () => {
  it('renders nothing for a post with no session', () => {
    const el = mount({ post: { id: 1 } })
    expect(el.querySelector('.sesspill')).toBeFalsy()
  })

  it('renders sessionLabel(post) text, falling back to the short id', () => {
    const post = { id: 1, session_id: 'abcdef1234567890' }
    const el = mount({ post })
    const pill = el.querySelector('.sesspill')
    expect(pill).toBeTruthy()
    expect(pill.textContent).toBe(sessionLabel(post))
    expect(pill.textContent).toBe('abcdef12')
  })

  it('prefers the server display name over the short id', () => {
    const post = { id: 1, session_id: 'abcdef1234567890', session_display_name: 'crimson-otter-7' }
    const el = mount({ post })
    expect(el.querySelector('.sesspill').textContent).toBe('crimson-otter-7')
  })

  it('applies sesspill-on when store.session matches the post session', () => {
    const post = { id: 1, session_id: 'abcdef1234567890' }
    S.session = 'abcdef1234567890'
    const el = mount({ post })
    expect(el.querySelector('.sesspill').classList.contains('sesspill-on')).toBe(true)
  })

  it('does not apply sesspill-on when store.session differs', () => {
    const post = { id: 1, session_id: 'abcdef1234567890' }
    S.session = 'some-other-session'
    const el = mount({ post })
    expect(el.querySelector('.sesspill').classList.contains('sesspill-on')).toBe(false)
  })

  it('does not apply sesspill-on when no session filter is active', () => {
    const post = { id: 1, session_id: 'abcdef1234567890' }
    S.session = ''
    const el = mount({ post })
    expect(el.querySelector('.sesspill').classList.contains('sesspill-on')).toBe(false)
  })

  it('renders a markup-shaped session name as text, never as markup', () => {
    const post = { id: 1, session_id: 'abcdef1234567890', session_display_name: '<i>y</i>' }
    const el = mount({ post })
    const pill = el.querySelector('.sesspill')
    expect(pill.querySelector('i')).toBeFalsy()
    expect(pill.textContent).toBe('<i>y</i>')
  })
})
