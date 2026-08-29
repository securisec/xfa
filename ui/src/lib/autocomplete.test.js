import { describe, it, expect } from 'vitest'
import { tokenAt, candidates, applyCompletion, MAX_CANDIDATES } from './autocomplete.js'

// A store-shaped literal is enough: candidates() only reads the loaded arrays
// (threads / thread / results / questions / inbox), never the reactive
// machinery, so the pure tests never need useStore().
function storeOf(over = {}) {
  return {
    threads: [], thread: [], results: [], questions: [], inbox: [],
    threadId: 0,
    ...over,
  }
}

function post(id, author, body, over = {}) {
  return { id, author, body, ...over }
}

describe('tokenAt', () => {
  it('finds a handle token the caret sits at the end of', () => {
    expect(tokenAt('@cri', 4)).toEqual({ start: 0, query: 'cri', kind: 'handle' })
  })

  it('finds a post-ref token behind the # sigil', () => {
    expect(tokenAt('#12', 3)).toEqual({ start: 0, query: '12', kind: 'post' })
    expect(tokenAt('see #123 now', 8)).toEqual({ start: 4, query: '123', kind: 'post' })
  })

  it('treats a bare sigil as an empty query, not as no token', () => {
    expect(tokenAt('@', 1)).toEqual({ start: 0, query: '', kind: 'handle' })
    expect(tokenAt('hello @', 7)).toEqual({ start: 6, query: '', kind: 'handle' })
    expect(tokenAt('#', 1)).toEqual({ start: 0, query: '', kind: 'post' })
    expect(tokenAt('hello #', 7)).toEqual({ start: 6, query: '', kind: 'post' })
  })

  it('finds a token mid-text, after whitespace', () => {
    expect(tokenAt('see @crimson-otter-7', 20))
      .toEqual({ start: 4, query: 'crimson-otter-7', kind: 'handle' })
  })

  // The sigil, not the query's shape, picks the kind now: the two namespaces
  // cannot be reached through the wrong sigil at all.
  it('never yields a post kind behind @, however numeric the query', () => {
    expect(tokenAt('@123', 4)).toEqual({ start: 0, query: '123', kind: 'handle' })
  })

  it('returns null for a # followed by a letter: that is prose, not a ref', () => {
    expect(tokenAt('#abc', 4)).toBeNull()
    expect(tokenAt('#a', 2)).toBeNull()
    expect(tokenAt('#12a', 4)).toBeNull()
  })

  it('stops the query at the caret, ignoring the rest of the token', () => {
    // '@crimson' with the caret after '@cri': the tail is not part of the query.
    expect(tokenAt('@crimson', 4)).toEqual({ start: 0, query: 'cri', kind: 'handle' })
    expect(tokenAt('#1234', 3)).toEqual({ start: 0, query: '12', kind: 'post' })
  })

  it('lowercases the query but matches token chars case-insensitively', () => {
    expect(tokenAt('@CriMSON', 8)).toEqual({ start: 0, query: 'crimson', kind: 'handle' })
  })

  it('accepts a token opened right after punctuation or a newline', () => {
    expect(tokenAt('(@ab', 4)).toEqual({ start: 1, query: 'ab', kind: 'handle' })
    expect(tokenAt('one\n@ab', 7)).toEqual({ start: 4, query: 'ab', kind: 'handle' })
    expect(tokenAt('a, @ab', 6)).toEqual({ start: 3, query: 'ab', kind: 'handle' })
    expect(tokenAt('(#12', 4)).toEqual({ start: 1, query: '12', kind: 'post' })
    expect(tokenAt('one\n#12', 7)).toEqual({ start: 4, query: '12', kind: 'post' })
  })

  it('returns null when the @ is preceded by a word char (email addresses)', () => {
    expect(tokenAt('email@x', 7)).toBeNull()
    expect(tokenAt('johnsmith1234@gmail', 19)).toBeNull()
    expect(tokenAt('9@x', 3)).toBeNull()
    expect(tokenAt('foo_@x', 6)).toBeNull()
  })

  it('returns null when the # is preceded by a word char or a slash (URL fragments)', () => {
    expect(tokenAt('issue#12', 8)).toBeNull()
    expect(tokenAt('https://x.com/a#12', 18)).toBeNull()
    expect(tokenAt('https://x.com/#12', 17)).toBeNull()
    expect(tokenAt('foo_#12', 7)).toBeNull()
  })

  it('returns null for &#123, which is an HTML entity and not a ref', () => {
    expect(tokenAt('&#123', 5)).toBeNull()
  })

  it('returns null for a doubled sigil, which is never a reference', () => {
    expect(tokenAt('@@x', 3)).toBeNull()
    expect(tokenAt('##12', 4)).toBeNull()
    expect(tokenAt('@#12', 4)).toBeNull()
    expect(tokenAt('#@ab', 4)).toBeNull()
  })

  it('returns null when there is no sigil before the caret', () => {
    expect(tokenAt('', 0)).toBeNull()
    expect(tokenAt('plain text', 10)).toBeNull()
    expect(tokenAt('a @b c', 6)).toBeNull()   // caret is past the token
    expect(tokenAt('a #1 c', 6)).toBeNull()
  })

  it('returns null when the token contains a char the kind never carries', () => {
    // The space ends the scan, so the char before the run is ' ', not a sigil.
    expect(tokenAt('@ab cd', 6)).toBeNull()
    expect(tokenAt('@ab.cd', 6)).toBeNull()
    expect(tokenAt('#12 34', 6)).toBeNull()
  })

  it('suppresses the menu inside an unclosed backtick code span', () => {
    expect(tokenAt('`@ab', 4)).toBeNull()
    expect(tokenAt('use `foo @ab', 12)).toBeNull()
    expect(tokenAt('`#12', 4)).toBeNull()
    expect(tokenAt('use `foo #12', 12)).toBeNull()
  })

  it('re-enables the menu once the code span is closed', () => {
    expect(tokenAt('`foo` @ab', 9)).toEqual({ start: 6, query: 'ab', kind: 'handle' })
    expect(tokenAt('`foo` #12', 9)).toEqual({ start: 6, query: '12', kind: 'post' })
  })

  it('counts backticks per line, so an earlier line cannot poison this one', () => {
    expect(tokenAt('`open\n@ab', 9)).toEqual({ start: 6, query: 'ab', kind: 'handle' })
  })

  it('clamps a caret outside the text instead of throwing', () => {
    expect(tokenAt('@ab', 99)).toEqual({ start: 0, query: 'ab', kind: 'handle' })
    expect(tokenAt('@ab', -3)).toBeNull()
  })

  it('returns null for junk input', () => {
    expect(tokenAt(null, 0)).toBeNull()
    expect(tokenAt('@ab', null)).toBeNull()
    expect(tokenAt('@ab', NaN)).toBeNull()
  })
})

describe('candidates', () => {
  const S = storeOf({
    threadId: 10,
    thread: [
      post(10, 'crimson-otter-7', 'root of the open thread'),
      post(11, 'azure-lynx-3', 'a reply in the open thread'),
    ],
    threads: [
      { root: post(20, 'crimson-otter-7', 'a thread list root'), replies: 4 },
      { root: post(21, 'gold-badger-1', 'another list root'), replies: 0 },
    ],
    results: [post(30, 'azure-lynx-3', 'a search hit')],
    questions: [post(40, 'violet-moth-9', 'a question')],
    inbox: [post(50, 'gold-badger-1', 'an inbox reply')],
  })

  it('returns handle candidates for a handle query', () => {
    const out = candidates(S, 'cri', 'handle')
    expect(out.length).toBe(1)
    expect(out[0]).toEqual({
      kind: 'handle', label: '@crimson-otter-7', insert: '@crimson-otter-7 ', detail: 'handle',
    })
  })

  it('returns post candidates for a post query', () => {
    const out = candidates(S, '2', 'post')
    expect(out.every((c) => c.kind === 'post')).toBe(true)
    expect(out.map((c) => c.label)).toEqual(['#21', '#20'])
    expect(out[0].insert).toBe('#21 ')
    expect(out[0].detail).toBe('another list root')
  })

  // The kind comes from the sigil, so a numeric query behind '@' stays in the
  // handle namespace (and finds nothing, since handles are never bare digits)
  // and a query behind '#' never reaches handles at all.
  it('keeps the two namespaces apart: the kind decides, not the query shape', () => {
    expect(candidates(S, '12', 'handle')).toEqual([])
    expect(candidates(S, 'otter-7', 'handle').map((c) => c.label)).toEqual(['@crimson-otter-7'])
    expect(candidates(S, 'otter-7', 'post')).toEqual([])
  })

  it('returns nothing for an unknown or missing kind', () => {
    expect(candidates(S, '', undefined)).toEqual([])
    expect(candidates(S, 'cri', 'nonsense')).toEqual([])
  })

  it('offers the open thread first for an empty post query', () => {
    const out = candidates(S, '', 'post')
    expect(out.every((c) => c.kind === 'post')).toBe(true)
    expect(out[0].label).toBe('#11')   // thread posts before every other source
    expect(out[1].label).toBe('#10')
  })

  it('offers every handle for an empty handle query', () => {
    const out = candidates(S, '', 'handle')
    expect(out.every((c) => c.kind === 'handle')).toBe(true)
    expect(out.map((c) => c.label)).toContain('@crimson-otter-7')
  })

  it('dedupes handles across sources and posts by id', () => {
    const dupes = storeOf({
      thread: [post(1, 'same-handle-1', 'a'), post(1, 'same-handle-1', 'a')],
      results: [post(1, 'same-handle-1', 'a')],
      inbox: [post(2, 'same-handle-1', 'b')],
    })
    expect(candidates(dupes, 'same', 'handle').map((c) => c.label)).toEqual(['@same-handle-1'])
    expect(candidates(dupes, '1', 'post').map((c) => c.label)).toEqual(['#1'])
  })

  it('caps the list at MAX_CANDIDATES', () => {
    const many = storeOf({
      results: Array.from({ length: 40 }, (_, i) => post(100 + i, 'h' + i, 'body ' + i)),
    })
    expect(MAX_CANDIDATES).toBe(8)
    expect(candidates(many, 'h', 'handle').length).toBe(MAX_CANDIDATES)
    expect(candidates(many, '1', 'post').length).toBe(MAX_CANDIDATES)
    expect(candidates(many, '', 'post').length).toBe(MAX_CANDIDATES)
    expect(candidates(many, '', 'handle').length).toBe(MAX_CANDIDATES)
  })

  it('sorts prefix matches ahead of interior matches, then alphabetically', () => {
    const s = storeOf({
      results: [
        post(1, 'zebra-ab-1', 'z'),
        post(2, 'ab-first-2', 'a'),
        post(3, 'ab-second-3', 'b'),
        post(4, 'moth-ab-4', 'm'),
      ],
    })
    expect(candidates(s, 'ab', 'handle').map((c) => c.label))
      .toEqual(['@ab-first-2', '@ab-second-3', '@moth-ab-4', '@zebra-ab-1'])
  })

  it('sorts post ids prefix-first, then newest id first', () => {
    const s = storeOf({
      results: [post(3, 'h', 'a'), post(13, 'h', 'b'), post(31, 'h', 'c'), post(30, 'h', 'd')],
    })
    expect(candidates(s, '3', 'post').map((c) => c.label)).toEqual(['#31', '#30', '#3', '#13'])
  })

  it('clips a long body to a short detail and marks the clip', () => {
    const s = storeOf({ results: [post(1, 'h', 'x'.repeat(200))] })
    const d = candidates(s, '1', 'post')[0].detail
    expect(d.length).toBeLessThanOrEqual(41)
    expect(d.endsWith('…')).toBe(true)
  })

  it('collapses whitespace in the detail so a multi-line body stays one row', () => {
    const s = storeOf({ results: [post(1, 'h', '  first line\n\nsecond   line  ')] })
    expect(candidates(s, '1', 'post')[0].detail).toBe('first line second line')
  })

  it('shows [deleted] as the detail of a tombstoned post', () => {
    const s = storeOf({ results: [post(1, 'h', 'whatever the server sent', { deleted: true })] })
    expect(candidates(s, '1', 'post')[0].detail).toBe('[deleted]')
  })

  it('harvests reply posts when a threads row carries them, and survives the count form', () => {
    // /api/boards/{slug}/threads sends `replies` as a NUMBER; the array form is
    // tolerated so a richer payload cannot crash the picker.
    const counted = storeOf({ threads: [{ root: post(1, 'root-handle-1', 'r'), replies: 7 }] })
    expect(candidates(counted, 'root', 'handle').map((c) => c.label)).toEqual(['@root-handle-1'])
    const nested = storeOf({
      threads: [{ root: post(1, 'root-handle-1', 'r'), replies: [post(2, 'kid-handle-2', 'k')] }],
    })
    expect(candidates(nested, 'kid', 'handle').map((c) => c.label)).toEqual(['@kid-handle-2'])
  })

  it('ignores malformed rows, missing arrays and a null store', () => {
    expect(candidates(null, 'a', 'handle')).toEqual([])
    expect(candidates(undefined, '', 'post')).toEqual([])
    expect(candidates(storeOf({ threads: [null, {}, { root: null }] }), '', 'post')).toEqual([])
    expect(candidates(storeOf({ results: 'not an array' }), 'a', 'handle')).toEqual([])
    expect(candidates(storeOf({ results: [{ id: 1 }, { author: 'no-id' }] }), 'no-id', 'handle'))
      .toEqual([])
  })

  it('never offers a handle-shaped candidate for a post with a blank author', () => {
    const s = storeOf({ results: [post(1, '', 'body')] })
    expect(candidates(s, '', 'handle')).toEqual([])
    expect(candidates(s, '', 'post')).toEqual([
      { kind: 'post', label: '#1', insert: '#1 ', detail: 'body' },
    ])
  })
})

describe('applyCompletion', () => {
  it('replaces the partial token and puts the caret after the insert', () => {
    const r = applyCompletion('@cri', 4, { start: 0, query: 'cri' }, '@crimson-otter-7 ')
    expect(r).toEqual({ text: '@crimson-otter-7 ', caret: 17 })
  })

  it('keeps the text on both sides of the token', () => {
    const r = applyCompletion('see @cri now', 8, { start: 4, query: 'cri' }, '@crimson-otter-7 ')
    // One space, not two: the insert's trailing separator is dropped because
    // the text already has one at the boundary.
    expect(r.text).toBe('see @crimson-otter-7 now')
    expect(r.caret).toBe(20)
    expect(r.text.slice(0, r.caret)).toBe('see @crimson-otter-7')
  })

  it('drops the appended space against any whitespace boundary, not just a space', () => {
    expect(applyCompletion('a @cri\nb', 6, { start: 2 }, '@crimson-otter-7 ').text)
      .toBe('a @crimson-otter-7\nb')
    expect(applyCompletion('a @cri\tb', 6, { start: 2 }, '@crimson-otter-7 ').text)
      .toBe('a @crimson-otter-7\tb')
  })

  it('keeps the appended space at the end of the text, where there is no boundary', () => {
    const r = applyCompletion('see @cri', 8, { start: 4 }, '@crimson-otter-7 ')
    expect(r.text).toBe('see @crimson-otter-7 ')
    expect(r.caret).toBe(21)
  })

  it('keeps the appended space against a non-whitespace boundary', () => {
    expect(applyCompletion('(@cri)', 5, { start: 1 }, '@crimson-otter-7 ').text)
      .toBe('(@crimson-otter-7 )')
  })

  it('leaves an insert that carries no trailing space alone', () => {
    expect(applyCompletion('a @cri b', 6, { start: 2 }, '@x').text).toBe('a @x b')
  })

  it('replaces only up to the caret, leaving the token tail in place', () => {
    const r = applyCompletion('@crimson', 4, { start: 0, query: 'cri' }, '#123 ')
    expect(r).toEqual({ text: '#123 mson', caret: 5 })
  })

  it('completes a bare @', () => {
    expect(applyCompletion('hi @', 4, { start: 3, query: '' }, '@ab-cd-1 '))
      .toEqual({ text: 'hi @ab-cd-1 ', caret: 12 })
  })

  it('completes a bare #, splicing over the sigil the same way', () => {
    expect(applyCompletion('hi #', 4, { start: 3, query: '', kind: 'post' }, '#42 '))
      .toEqual({ text: 'hi #42 ', caret: 7 })
    expect(applyCompletion('see #1 now', 6, { start: 4, query: '1', kind: 'post' }, '#12 ').text)
      .toBe('see #12 now')
  })

  it('clamps nonsense offsets instead of producing garbage', () => {
    expect(applyCompletion('@ab', 99, { start: 0 }, '@x ')).toEqual({ text: '@x ', caret: 3 })
    // start past the caret: the pair is ordered rather than trusted.
    expect(applyCompletion('@ab', 1, { start: 3 }, '@x ')).toEqual({ text: '@@x ', caret: 4 })
    expect(applyCompletion(null, 0, null, null)).toEqual({ text: '', caret: 0 })
  })
})
