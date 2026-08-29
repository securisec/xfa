import { describe, it, expect } from 'vitest'
import {
  runes,
  rel,
  handleColor,
  tagStyle,
  sessionLabel,
  slugOf,
  HANDLE_COLORS,
  TAG_COLORS,
  TAG_OTHER,
  BODY_CLIP,
  MAX_SESSION_NAME,
} from './format.js'

describe('runes', () => {
  it('counts runes not UTF-16 units', () => {
    expect(runes('héllo')).toBe(5)
    expect(runes('🐙🐙')).toBe(2)   // 4 UTF-16 units, 2 runes
    expect(runes('')).toBe(0)
    expect(runes(null)).toBe(0)
  })
})

describe('handleColor', () => {
  it('is stable and drawn from the shared palette', () => {
    const c = handleColor('crimson-otter-7')
    expect(HANDLE_COLORS).toContain(c)
    expect(handleColor('crimson-otter-7')).toBe(c)
  })

  it('treats a nullish handle as an empty string', () => {
    expect(HANDLE_COLORS).toContain(handleColor(null))
    expect(handleColor(null)).toBe(handleColor(undefined))
    expect(handleColor(null)).toBe(handleColor(''))
  })
})

describe('constants', () => {
  it('pins the product ceilings', () => {
    expect(BODY_CLIP).toBe(500)
    expect(MAX_SESSION_NAME).toBe(60)
    expect(TAG_COLORS.question).toBe('var(--ctp-yellow)')
  })
})

describe('rel', () => {
  it('renders a relative timestamp', () => {
    expect(rel(new Date().toISOString())).toMatch(/now|s$|m$/)
  })

  it('returns empty string for falsy input', () => {
    expect(rel(null)).toBe('')
    expect(rel('')).toBe('')
    expect(rel(undefined)).toBe('')
  })

  it('returns empty string for an unparseable timestamp', () => {
    expect(rel('not-a-date')).toBe('')
  })

  it('steps through seconds, minutes, hours and days', () => {
    const now = Date.now()
    expect(rel(new Date(now - 5 * 1000).toISOString())).toBe('5s')
    expect(rel(new Date(now - 5 * 60 * 1000).toISOString())).toBe('5m')
    expect(rel(new Date(now - 5 * 60 * 60 * 1000).toISOString())).toBe('5h')
    expect(rel(new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString())).toBe('5d')
  })
})

describe('tagStyle', () => {
  it('paints a known tag in its palette color at ~15% alpha', () => {
    expect(tagStyle('question')).toBe('color:var(--ctp-yellow);background:color-mix(in oklab, var(--ctp-yellow) 15%, transparent)')
  })

  it('falls back to TAG_OTHER for an unknown tag', () => {
    expect(tagStyle('custom')).toBe(
      'color:' + TAG_OTHER + ';background:color-mix(in oklab, ' + TAG_OTHER + ' 15%, transparent)',
    )
  })
})

describe('sessionLabel', () => {
  it('returns empty string when there is no post or no session_id', () => {
    expect(sessionLabel(null)).toBe('')
    expect(sessionLabel(undefined)).toBe('')
    expect(sessionLabel({})).toBe('')
    expect(sessionLabel({ session_id: '' })).toBe('')
  })

  it('prefers session_display_name when present', () => {
    expect(sessionLabel({ session_id: 'abcdefgh12345', session_display_name: 'my-session' })).toBe('my-session')
  })

  it('falls back to the first 8 chars of session_id', () => {
    expect(sessionLabel({ session_id: 'abcdefgh12345' })).toBe('abcdefgh')
  })
})

describe('slugOf', () => {
  const boards = [
    { id: 1, slug: 'agents-messageboard' },
    { id: 2, slug: 'general' },
  ]

  it('finds the slug for a known board id', () => {
    expect(slugOf(2, boards)).toBe('general')
  })

  it('falls back to "?" for an unknown board id', () => {
    expect(slugOf(999, boards)).toBe('?')
  })
})
