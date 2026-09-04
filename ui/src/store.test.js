import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useStore } from './store.js'

beforeEach(() => { vi.restoreAllMocks() })

describe('hash routing', () => {
  it('routeHash/parseHash round-trip every view', () => {
    const S = useStore()
    for (const setup of [
      () => { S.view = 'threads'; S.board = ''; S.session = '' },
      () => { S.view = 'threads'; S.board = 'b/xfa'; S.session = 'abc123' },
      () => { S.view = 'thread'; S.board = 'b/xfa'; S.threadId = 42 },
      () => { S.view = 'search'; S.q = 'hello world' },
      () => { S.view = 'questions' }, () => { S.view = 'inbox' },
      () => { S.view = 'myposts' }, () => { S.view = 'stats' },
    ]) {
      setup()
      const before = { view: S.view, board: S.board, threadId: S.threadId, q: S.q, session: S.session }
      const r = S.parseHash(S.routeHash())
      S.applyRoute(r)
      expect({ view: S.view, board: S.board, threadId: S.threadId, q: S.q, session: S.session }).toEqual(before)
    }
  })

  it('parseHash returns null for a bare or unknown URL', () => {
    const S = useStore()
    expect(S.parseHash('')).toBeNull()
    expect(S.parseHash('#')).toBeNull()
    expect(S.parseHash('#/nope')).toBeNull()
    expect(S.parseHash('#/t/abc')).toBeNull()   // ids are digits only
    expect(S.parseHash('#/')).toEqual({ view: 'threads', board: '' })
  })

  it('applyRoute drops the session filter when the board changes', () => {
    const S = useStore()
    S.view = 'threads'; S.board = 'b/one'; S.session = 'sess-1'
    S.applyRoute({ view: 'threads', board: 'b/two' })
    expect(S.session).toBe('')
    expect(S.board).toBe('b/two')
  })

  it('applyRoute clears thread payload/reply/expanded only for a different thread', () => {
    const S = useStore()
    S.view = 'thread'; S.threadId = 7; S.thread = [{ id: 7 }]; S.reply.body = 'draft'; S.expanded = { 7: true }
    S.applyRoute({ view: 'thread', threadId: 7 })
    expect(S.thread.length).toBe(1)
    expect(S.reply.body).toBe('draft')
    expect(S.expanded[7]).toBe(true)
    S.applyRoute({ view: 'thread', threadId: 8 })
    expect(S.thread).toEqual([])
    expect(S.reply.body).toBe('')
    expect(S.expanded).toEqual({})
  })

  it('syncHash writes the hash and skips a redundant write', () => {
    const S = useStore()
    S.view = 'questions'
    S.syncHash()
    expect(location.hash).toBe('#/questions')
    const before = location.hash
    S.syncHash()
    expect(location.hash).toBe(before)
  })

  it('onHashChange ignores unknown routes and the no-op case', () => {
    const S = useStore()
    const spy = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    location.hash = '#/nope'
    S.onHashChange()
    expect(spy).not.toHaveBeenCalled()
    S.view = 'stats'
    location.hash = '#/stats'
    S.onHashChange()          // loop guard: routeHash() already equals location.hash
    expect(spy).not.toHaveBeenCalled()
    S.view = 'threads'; S.board = ''
    location.hash = '#/inbox'
    S.onHashChange()
    expect(S.view).toBe('inbox')
    expect(spy).toHaveBeenCalledWith(true)
  })
})

describe('send', () => {
  it('surfaces API {error} payloads as thrown Errors', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'nope' }), { status: 403 })))
    const { send } = await import('./lib/api.js')
    await expect(send('POST', '/api/posts', {})).rejects.toThrow('nope')
  })

  it('falls back to the raw text of a non-JSON error body', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('forbidden host', { status: 403 })))
    const { send } = await import('./lib/api.js')
    await expect(send('POST', '/api/posts', {})).rejects.toThrow('forbidden host')
  })

  it('store.send raises the toast and rethrows', async () => {
    const S = useStore()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 })))
    const spy = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    await expect(S.send('POST', '/api/posts', { body: 'x' })).rejects.toThrow('boom')
    expect(S.error).toBe('boom')
    expect(spy).not.toHaveBeenCalled()   // no refresh on a failed write
  })

  it('store.send refreshes after a successful write', async () => {
    const S = useStore()
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ deleted: 3 }), { status: 200 })))
    const spy = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    await expect(S.send('DELETE', '/api/posts/1', null)).resolves.toEqual({ deleted: 3 })
    expect(spy).toHaveBeenCalledWith(true)
  })
})

describe('toasts', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('fail() shows the message and clears it after 5s', () => {
    const S = useStore()
    S.fail(new Error('bad'))
    expect(S.error).toBe('bad')
    vi.advanceTimersByTime(5000)
    expect(S.error).toBe('')
  })

  it('ok() shows the notice and clears it after 2.5s', () => {
    const S = useStore()
    S.ok('Posted')
    expect(S.notice).toBe('Posted')
    vi.advanceTimersByTime(2500)
    expect(S.notice).toBe('')
  })
})

describe('query builders', () => {
  it('emit nothing when unset and encode when set', () => {
    const S = useStore()
    S.board = ''; S.session = ''
    expect(S.boardQuery('?')).toBe('')
    expect(S.sessionQuery('?')).toBe('')
    S.board = 'b/x y'; S.session = 'a/b'
    expect(S.boardQuery('?')).toBe('?board=b%2Fx%20y')
    expect(S.sessionQuery('&')).toBe('&session=a%2Fb')
  })
})

describe('refresh', () => {
  beforeEach(() => {
    const S = useStore()
    S.busy = false; S.board = ''; S.session = ''; S.sessions = []; S.threads = []
  })

  it('defers a poll to an in-flight load but never a forced one', async () => {
    const S = useStore()
    S.busy = true
    const f = vi.fn()
    vi.stubGlobal('fetch', f)
    await S.refresh()
    expect(f).not.toHaveBeenCalled()
    S.busy = false
  })

  it('discards a superseded run rather than writing its response', async () => {
    const S = useStore()
    S.view = 'threads'
    S.board = 'b/x'
    S.boards = []
    let release
    const gate = new Promise((r) => { release = r })
    vi.stubGlobal('fetch', vi.fn(async (url) => {
      if (String(url) === '/api/boards') { await gate; return new Response(JSON.stringify([{ id: 1, slug: 'b/x' }])) }
      return new Response(JSON.stringify([]))
    }))
    const slow = S.refresh(true)          // claims seq N
    S.seq++                               // a newer run supersedes it
    release()
    await slow
    expect(S.boards).toEqual([])          // stale payload discarded
    expect(S.busy).toBe(true)             // the flag belongs to the newer run
    S.busy = false
  })

  it('clears the session list on the all-boards view', async () => {
    const S = useStore()
    S.view = 'threads'; S.board = ''; S.sessions = [{ session_id: 'a' }]; S.session = 'a'
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([]))))
    await S.refresh(true)
    expect(S.sessions).toEqual([])
    expect(S.session).toBe('')
  })
})

// Every branch of refresh's per-view fan-out, pinned at the URL: which endpoint
// each view calls, the '?' vs '&' separator each query builder is handed, the
// encoding of a slug/session/query that needs it, and the trailing /api/sessions
// call that follows whenever a board is active. /api/boards always leads.
describe('refresh fan-out', () => {
  const cases = [
    {
      name: 'threads with a board and a session filter',
      state: { view: 'threads', board: 'b/x', session: 's 1', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/boards/b%2Fx/threads?session=s%201', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'threads with a board and no session filter',
      state: { view: 'threads', board: 'b/x', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/boards/b%2Fx/threads', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'threads with no board loads nothing but the board list',
      state: { view: 'threads', board: '', session: '', threadId: 0, q: '' },
      urls: ['/api/boards'],
    },
    {
      name: 'thread by id, unaffected by the board query',
      state: { view: 'thread', board: '', session: '', threadId: 42, q: '' },
      urls: ['/api/boards', '/api/threads/42'],
    },
    {
      name: 'thread with no id skips the thread load',
      state: { view: 'thread', board: '', session: '', threadId: 0, q: '' },
      urls: ['/api/boards'],
    },
    {
      name: 'search appends the board with & after its own ?q=',
      state: { view: 'search', board: 'b/x', session: '', threadId: 0, q: 'hello world' },
      urls: ['/api/boards', '/api/search?q=hello%20world&board=b%2Fx', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'search with a blank query skips the search',
      state: { view: 'search', board: '', session: '', threadId: 0, q: '   ' },
      urls: ['/api/boards'],
    },
    {
      name: 'questions takes the board as its leading ?',
      state: { view: 'questions', board: 'b/x', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/questions?board=b%2Fx', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'questions with no board emits no query at all',
      state: { view: 'questions', board: '', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/questions'],
    },
    {
      name: 'inbox is never board-scoped',
      state: { view: 'inbox', board: 'b/x', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/inbox', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'myposts is board-scoped',
      state: { view: 'myposts', board: 'b/x', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/myposts?board=b%2Fx', '/api/sessions?board=b%2Fx'],
    },
    {
      name: 'myposts with no board emits no query at all',
      state: { view: 'myposts', board: '', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/myposts'],
    },
    {
      name: 'stats takes the board as its leading ?',
      state: { view: 'stats', board: 'b/x', session: '', threadId: 0, q: '' },
      urls: ['/api/boards', '/api/stats?board=b%2Fx', '/api/sessions?board=b%2Fx'],
    },
  ]

  for (const c of cases) {
    it(c.name, async () => {
      const S = useStore()
      Object.assign(S, { busy: false, sessions: [] }, c.state)
      const f = vi.fn(async () => new Response(JSON.stringify([])))
      vi.stubGlobal('fetch', f)
      await S.refresh(true)
      expect(f.mock.calls.map((a) => String(a[0]))).toEqual(c.urls)
    })
  }
})

describe('deleteBlastMessage', () => {
  it('states blast radius before hard deletes', () => {
    const S = useStore()
    expect(S.deleteBlastMessage('thread', 3)).toMatch(/3/)
    expect(S.deleteBlastMessage('post', 0)).toBeTruthy()
    expect(S.deleteBlastMessage('post', 1)).toMatch(/1 reply\?$/)
    expect(S.deleteBlastMessage('thread', 2)).toMatch(/2 replies\?$/)
  })
})

describe('threadRows', () => {
  it('flattens with depth and survives a parent cycle', () => {
    const S = useStore()
    S.thread = [
      { id: 1, parent_id: null, created_at: '2026-01-01T00:00:00Z' },
      { id: 2, parent_id: 1, created_at: '2026-01-01T00:01:00Z' },
      { id: 3, parent_id: 2, created_at: '2026-01-01T00:02:00Z' },
    ]
    S.threadId = 1
    const rows = S.threadRows()
    expect(rows.map((r) => r.post.id)).toEqual([1, 2, 3])
    expect(rows[2].depth).toBe(2)
    S.thread = [{ id: 1, parent_id: 2, created_at: '2026-01-01' }, { id: 2, parent_id: 1, created_at: '2026-01-01' }]
    expect(() => S.threadRows()).not.toThrow()  // cycle guard
  })

  it('renders orphans at depth 1 and sorts siblings by time then id', () => {
    const S = useStore()
    S.thread = [
      { id: 1, parent_id: null, created_at: '2026-01-01T00:00:00Z' },
      { id: 3, parent_id: 1, created_at: '2026-01-01T00:02:00Z' },
      { id: 2, parent_id: 1, created_at: '2026-01-01T00:01:00Z' },
      { id: 9, parent_id: 404, created_at: '2026-01-01T00:03:00Z' },  // parent not in payload
    ]
    const rows = S.threadRows()
    expect(rows.map((r) => r.post.id)).toEqual([1, 2, 3, 9])
    expect(rows.map((r) => r.depth)).toEqual([0, 1, 1, 1])
  })

  it('threadRoot picks the parentless post', () => {
    const S = useStore()
    S.thread = [{ id: 2, parent_id: 1 }, { id: 1, parent_id: null }]
    expect(S.threadRoot().id).toBe(1)
    S.thread = []
    expect(S.threadRoot()).toBeNull()
  })
})

describe('descendantCount', () => {
  it('counts the whole subtree and survives a cycle', () => {
    const S = useStore()
    S.thread = [
      { id: 1, parent_id: null }, { id: 2, parent_id: 1 },
      { id: 3, parent_id: 2 }, { id: 4, parent_id: 1 },
    ]
    expect(S.descendantCount(1)).toBe(3)
    expect(S.descendantCount(2)).toBe(1)
    expect(S.descendantCount(4)).toBe(0)
    S.thread = [{ id: 1, parent_id: 2 }, { id: 2, parent_id: 1 }]
    expect(() => S.descendantCount(1)).not.toThrow()
  })
})

describe('canResolve', () => {
  it('is true only for an open, live, top-level question', () => {
    const S = useStore()
    expect(S.canResolve({ tag: 'question' })).toBe(true)
    expect(S.canResolve({ tag: 'til' })).toBe(false)
    expect(S.canResolve({ tag: 'question', resolved_at: 'x' })).toBe(false)
    expect(S.canResolve({ tag: 'question', parent_id: 4 })).toBe(false)
    expect(S.canResolve({ tag: 'question', deleted: true })).toBe(false)
  })
  it('is true for any live, open human-authored post regardless of tag or depth', () => {
    const S = useStore()
    expect(S.canResolve({ tag: '', human: true })).toBe(true)
    expect(S.canResolve({ tag: '', parent_id: 4, human: true })).toBe(true)
    expect(S.canResolve({ tag: '', human: true, resolved_at: 'x' })).toBe(false)
    expect(S.canResolve({ tag: '', human: true, deleted: true })).toBe(false)
    expect(S.canResolve({ tag: 'til' })).toBe(false)
  })
})

describe('session filter', () => {
  it('pruneSession drops a selection the board has never heard of', () => {
    const S = useStore()
    S.sessions = [{ session_id: 'a' }, { session_id: 'b' }]
    S.session = 'b'
    S.pruneSession()
    expect(S.session).toBe('b')
    S.session = 'zz'
    S.pruneSession()
    expect(S.session).toBe('')
  })

  it('sessionRow and sessionOn key off the picked session', () => {
    const S = useStore()
    S.sessions = [{ session_id: 'a', name: 'alpha' }]
    S.session = 'a'
    expect(S.sessionRow('a').name).toBe('alpha')
    expect(S.sessionRow('nope')).toBeNull()
    expect(S.sessionOn({ session_id: 'a' })).toBe('sesspill-on')
    expect(S.sessionOn({ session_id: 'b' })).toBe('')
    S.session = ''
    expect(S.sessionOn({ session_id: 'a' })).toBe('')
  })

  it('pickSession lands on the thread list and forces a load', () => {
    const S = useStore()
    const spy = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.view = 'stats'
    S.pickSession('a')
    expect(S.session).toBe('a')
    expect(S.view).toBe('threads')
    expect(spy).toHaveBeenCalledWith(true)
    S.pickSession(undefined)
    expect(S.session).toBe('')
  })

  it('openRename seeds the stored name, never the fallback label', () => {
    const S = useStore()
    S.sessions = [{ session_id: 'a', display_name: 'fallback-ish' }]
    S.session = ''
    S.openRename()
    expect(S.rename.open).toBe(false)   // no selection: no dialog
    S.session = 'a'
    S.openRename()
    expect(S.rename.open).toBe(true)
    expect(S.rename.session).toBe('a')
    expect(S.rename.name).toBe('')
    S.sessions = [{ session_id: 'a', name: 'alpha' }]
    S.openRename()
    expect(S.rename.name).toBe('alpha')
    S.rename.open = false
  })
})

describe('navigation', () => {
  it('pickBoard drops the session filter and its rows when the board changes', () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.board = 'b/one'; S.session = 's'; S.sessions = [{ session_id: 's' }]
    S.pickBoard('b/two')
    expect(S.session).toBe('')
    expect(S.sessions).toEqual([])
    expect(S.view).toBe('threads')
    expect(S.board).toBe('b/two')
  })

  it('openThread resets the thread pane and the expanded set', () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.threadId = 1; S.expanded = { 5: true }; S.reply.body = 'draft'
    S.openThread(2)
    expect(S.expanded).toEqual({})
    expect(S.thread).toEqual([])
    expect(S.reply.body).toBe('')
    expect(S.threadId).toBe(2)
    expect(S.view).toBe('thread')
  })

  it('openPost resolves a reply to its thread root through the board grouping', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.boards = [{ id: 7, slug: 'b/x' }]
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([
      [{ id: 10 }, { id: 11 }, { id: 12 }],
    ]))))
    await S.openPost({ id: 12, parent_id: 11, board_id: 7 })
    expect(S.threadId).toBe(10)
  })

  // slugOf() answers '?' for an unknown board id — never a falsy value — so
  // the old `if (!slug)` short-circuit is unreachable and the lookup is
  // attempted against '?'. The 404 that comes back is what actually drives
  // the fallback: toast, then open the parent id.
  it('openPost falls back to the parent id when the board lookup fails', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.boards = []
    const f = vi.fn(async () => new Response(JSON.stringify({ error: 'no such board' }), { status: 404 }))
    vi.stubGlobal('fetch', f)
    await S.openPost({ id: 12, parent_id: 11, board_id: 7 })
    expect(f.mock.calls[0][0]).toBe('/api/boards/%3F/board')
    expect(S.error).toBe('no such board')
    expect(S.threadId).toBe(11)
  })

  it('openPost falls back to the parent id when no group contains the post', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.boards = [{ id: 7, slug: 'b/x' }]
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([[{ id: 99 }]]))))
    await S.openPost({ id: 12, parent_id: 11, board_id: 7 })
    expect(S.threadId).toBe(11)
  })

  it('openPost opens a root post directly', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    await S.openPost({ id: 3, parent_id: null })
    expect(S.threadId).toBe(3)
  })
})

// A #id click carries a globally unique post id, which the server can resolve
// whether or not a post_links row was ever recorded for it. openRef is the one
// entry point both the body anchors and the backlink chips go through: a known
// thread root wins (it is correct across boards), otherwise the bare id is
// resolved against whatever is already loaded, then against the board grouping
// (which maps a reply id to its root), and only failing all that opened
// optimistically as a root.
describe('openRef', () => {
  beforeEach(() => {
    const S = useStore()
    // Reset the fields openRef reads so leftover state from other suites (a
    // stale board slug especially) can't steer the not-loaded branch.
    S.thread = []; S.threads = []; S.results = []; S.questions = []; S.inbox = []; S.myposts = []
    S.board = ''; S.boards = []
  })

  it('opens the recorded link root when one is known', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn()
    vi.stubGlobal('fetch', f)
    const spy = vi.spyOn(S, 'openThread')
    await S.openRef(5, 2)
    expect(spy).toHaveBeenCalledWith(2)
    expect(S.threadId).toBe(2)
    expect(f).not.toHaveBeenCalled()
  })

  it('delegates to openPost when the id is already loaded (reply resolved to its root)', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const target = { id: 12, parent_id: 11, board_id: 7 }
    S.thread = []; S.threads = []; S.results = []; S.questions = []; S.inbox = [target]
    const spy = vi.spyOn(S, 'openPost').mockResolvedValue(undefined)
    await S.openRef(12, null)
    expect(spy).toHaveBeenCalledTimes(1)
    // toEqual, not toBe: the store is reactive(), so anything read back out of
    // it is a proxy of the object that was put in, never the raw object.
    expect(spy.mock.calls[0][0]).toEqual(target)
    S.inbox = []
  })

  it('finds the id in the loaded thread list roots', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const root = { id: 30, parent_id: null }
    S.thread = []; S.threads = [{ root: root, replies: 0 }]; S.results = []; S.questions = []; S.inbox = []
    const spy = vi.spyOn(S, 'openPost').mockResolvedValue(undefined)
    await S.openRef(30, null)
    expect(spy.mock.calls[0][0]).toEqual(root)
    S.threads = []
  })

  // The common case the fast path can't cover: a reply id nobody has loaded.
  // The board grouping maps it to its root (roots come first in each group),
  // so the thread endpoint is never hit with a bare reply id.
  it('resolves a not-loaded reply id to its root via the board grouping', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    // A loaded thread supplies the board to search: its root is on board 7.
    S.boards = [{ id: 7, slug: 'b/x' }]
    S.thread = [{ id: 1, parent_id: null, board_id: 7 }]
    const f = vi.fn(async () => new Response(JSON.stringify([
      [{ id: 1 }, { id: 3 }],   // reply #3 sits under root #1
    ])))
    vi.stubGlobal('fetch', f)
    const spy = vi.spyOn(S, 'openThread')
    await S.openRef(3, null)
    expect(f.mock.calls[0][0]).toBe('/api/boards/b%2Fx/board')
    expect(spy).toHaveBeenCalledWith(1)
    expect(S.threadId).toBe(1)
  })

  it('falls back to openThread(id) when the grouping does not contain the id', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.board = 'b/x'; S.boards = [{ id: 7, slug: 'b/x' }]
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([[{ id: 99 }]]))))
    const spy = vi.spyOn(S, 'openThread')
    await S.openRef(42, null)
    expect(spy).toHaveBeenCalledWith(42)
    expect(S.threadId).toBe(42)
  })

  it('falls back to openThread(id) when the grouping fetch rejects (no throw)', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.board = 'b/x'; S.boards = [{ id: 7, slug: 'b/x' }]
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('network down') }))
    const spy = vi.spyOn(S, 'openThread')
    await expect(S.openRef(55, null)).resolves.toBeUndefined()
    expect(spy).toHaveBeenCalledWith(55)
    expect(S.threadId).toBe(55)
  })

  it('optimistically opens the id as a thread root when no board is determinable', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    // beforeEach cleared thread/board/boards, so no slug can be found: no fetch.
    const f = vi.fn()
    vi.stubGlobal('fetch', f)
    const spy = vi.spyOn(S, 'openThread')
    await S.openRef(77, null)
    expect(f).not.toHaveBeenCalled()
    expect(spy).toHaveBeenCalledWith(77)
    expect(S.threadId).toBe(77)
    expect(S.view).toBe('thread')
  })

  it('is a no-op for a non-finite, zero or negative id', async () => {
    const S = useStore()
    const refresh = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn()
    vi.stubGlobal('fetch', f)
    S.view = 'threads'; S.threadId = 1
    for (const bad of [NaN, 0, -3, undefined, null]) {
      await S.openRef(bad, null)
    }
    expect(S.view).toBe('threads')
    expect(S.threadId).toBe(1)
    expect(refresh).not.toHaveBeenCalled()
    expect(f).not.toHaveBeenCalled()
  })
})

describe('writes', () => {
  it('openComposer defaults to the current board, then the first board', () => {
    const S = useStore()
    S.board = 'b/cur'; S.boards = [{ id: 1, slug: 'b/first' }]
    S.openComposer()
    expect(S.composer.board).toBe('b/cur')
    S.board = ''
    S.openComposer()
    expect(S.composer.board).toBe('b/first')
    S.boards = []
    S.openComposer()
    expect(S.composer.board).toBe('')
    S.composer.open = false
  })

  it('doPost clears the draft on success and keeps it on failure', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    S.composer.open = true; S.composer.board = 'b/x'; S.composer.body = 'hi'; S.composer.tag = ' til '
    const f = vi.fn(async () => new Response(JSON.stringify({ id: 1 })))
    vi.stubGlobal('fetch', f)
    await S.doPost()
    expect(JSON.parse(f.mock.calls[0][1].body)).toEqual({ board: 'b/x', body: 'hi', tag: 'til' })
    expect(S.composer.open).toBe(false)
    expect(S.composer.body).toBe('')
    expect(S.notice).toBe('Posted')
    expect(S.sending).toBe(false)

    S.composer.open = true; S.composer.body = 'keep me'
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'nope' }), { status: 400 })))
    await S.doPost()
    expect(S.composer.open).toBe(true)
    expect(S.composer.body).toBe('keep me')
    expect(S.error).toBe('nope')
    expect(S.sending).toBe(false)
  })

  it('doReply needs a thread and clears the draft on success', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn(async () => new Response(JSON.stringify({ id: 2 })))
    vi.stubGlobal('fetch', f)
    S.threadId = 0; S.reply.body = 'x'
    await S.doReply()
    expect(f).not.toHaveBeenCalled()
    S.threadId = 5
    await S.doReply()
    expect(f.mock.calls[0][0]).toBe('/api/posts/5/reply')
    expect(S.reply.body).toBe('')
    expect(S.notice).toBe('Replied')
  })

  it('doRename posts the trimmed name and closes only on success', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn(async () => new Response(JSON.stringify({ ok: true })))
    vi.stubGlobal('fetch', f)
    S.rename = { open: true, session: 'a/b', name: '  alpha  ' }
    await S.doRename()
    expect(f.mock.calls[0][0]).toBe('/api/sessions/a%2Fb/name')
    expect(JSON.parse(f.mock.calls[0][1].body)).toEqual({ name: 'alpha' })
    expect(S.rename.open).toBe(false)
    expect(S.notice).toBe('Renamed')

    S.rename = { open: true, session: 'a', name: '   ' }
    f.mockClear()
    await S.doRename()
    expect(f).not.toHaveBeenCalled()   // empty name is a no-op
    expect(S.rename.open).toBe(true)
    S.rename.open = false
  })

  it('doResolve swallows the error the toast already raised', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'gone' }), { status: 404 })))
    await expect(S.doResolve(4)).resolves.toBeUndefined()
    expect(S.error).toBe('gone')
  })
})

describe('confirm dialog', () => {
  it('resolves with the answer and closes', async () => {
    const S = useStore()
    const p = S.confirm({ title: 'delete post', body: 'Delete this post?' })
    expect(S.dialog.open).toBe(true)
    expect(S.dialog.action).toBe('delete')
    S.answerDialog(true)
    expect(await p).toBe(true)
    expect(S.dialog.open).toBe(false)
    expect(S.dialog.resolve).toBe(null)
    const q = S.confirm({ title: 'x', body: 'y', action: 'go', danger: false })
    S.answerDialog(false)
    expect(await q).toBe(false)
    S.answerDialog(false)   // no-op when nothing is pending
  })
})

describe('deletes', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('doDeletePost does nothing when the human cancels', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn()
    vi.stubGlobal('fetch', f)
    vi.spyOn(S, 'confirm').mockResolvedValue(false)
    await S.doDeletePost({ id: 1, parent_id: null })
    expect(f).not.toHaveBeenCalled()
  })

  it('doDeletePost leaves the thread view pre-emptively and reports the count', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    vi.spyOn(S, 'confirm').mockResolvedValue(true)
    const f = vi.fn(async () => new Response(JSON.stringify({ deleted: 4 })))
    vi.stubGlobal('fetch', f)
    S.view = 'thread'; S.threadId = 1; S.thread = [{ id: 1, parent_id: null }, { id: 2, parent_id: 1 }]
    await S.doDeletePost({ id: 1, parent_id: null })
    expect(f.mock.calls[0][0]).toBe('/api/posts/1')
    expect(f.mock.calls[0][1].method).toBe('DELETE')
    expect(S.view).toBe('threads')
    expect(S.thread).toEqual([])
    expect(S.notice).toBe('Deleted 4 post(s)')
  })

  it('doDeletePost restores the thread view when the delete fails', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    vi.spyOn(S, 'confirm').mockResolvedValue(true)
    const f = vi.fn(async () => new Response(JSON.stringify({ error: 'boom' }), { status: 500 }))
    vi.stubGlobal('fetch', f)
    S.view = 'thread'; S.threadId = 1; S.thread = [{ id: 1, parent_id: null }]
    await S.doDeletePost({ id: 1, parent_id: null })
    expect(f.mock.calls[0][1].method).toBe('DELETE')
    expect(S.view).toBe('thread')
    expect(S.error).toBe('boom')
  })

  it('doDeleteThread uses the summary reply count', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const c = vi.spyOn(S, 'confirm').mockResolvedValue(true)
    const f = vi.fn(async () => new Response(JSON.stringify({ deleted: 3 })))
    vi.stubGlobal('fetch', f)
    await S.doDeleteThread({ replies: 2, root: { id: 9 } })
    expect(f.mock.calls[0][0]).toBe('/api/posts/9')
    expect(f.mock.calls[0][1].method).toBe('DELETE')
    expect(c.mock.calls[0][0].body).toMatch(/2 replies/)
    expect(S.notice).toBe('Deleted 3 post(s)')
  })

  it('doDeleteSession clears the filter it is deleting', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    vi.spyOn(S, 'confirm').mockResolvedValue(true)
    const f = vi.fn(async () => new Response(JSON.stringify({ deleted: 7 })))
    vi.stubGlobal('fetch', f)
    S.session = 'a/b'
    await S.doDeleteSession({ session_id: 'a/b', display_name: 'alpha' })
    expect(S.session).toBe('')
    expect(f.mock.calls[0][0]).toBe('/api/sessions/a%2Fb')
    expect(f.mock.calls[0][1].method).toBe('DELETE')
    expect(S.notice).toBe('Deleted 7 post(s)')
  })

  it('deleteSelectedSession resolves the row from the picker list', async () => {
    const S = useStore()
    const spy = vi.spyOn(S, 'doDeleteSession').mockResolvedValue(undefined)
    S.sessions = [{ session_id: 'a', display_name: 'alpha' }]
    S.session = 'nope'
    S.deleteSelectedSession()
    expect(spy).not.toHaveBeenCalled()
    S.session = 'a'
    S.deleteSelectedSession()
    expect(spy).toHaveBeenCalledWith({ session_id: 'a', display_name: 'alpha' })
  })
})

describe('init', () => {
  afterEach(() => { const S = useStore(); clearInterval(S.timer); S.timer = null })

  it('lets the URL win over /api/me and starts the 5s poll', async () => {
    const S = useStore()
    vi.useFakeTimers()
    try {
      const refresh = vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
      vi.stubGlobal('fetch', vi.fn(async (url) => {
        if (String(url) === '/api/me') return new Response(JSON.stringify({ handle: 'human-1', board: 'b/me' }))
        return new Response(JSON.stringify([]))
      }))
      location.hash = '#/b/b%2Furl'
      await S.init()
      expect(S.me.handle).toBe('human-1')
      expect(S.board).toBe('b/url')      // the hash wins over /api/me's board
      expect(refresh).toHaveBeenCalledWith(true)
      refresh.mockClear()
      vi.advanceTimersByTime(5000)
      expect(refresh).toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('inline reply', () => {
  it('openInline toggles and switching posts drops the old draft', () => {
    const S = useStore()
    S.closeInline()
    S.openInline(7); S.inline.body = 'draft'
    expect(S.inline.parentId).toBe(7)
    S.openInline(9)
    expect(S.inline.parentId).toBe(9)
    expect(S.inline.body).toBe('')
    S.openInline(9)
    expect(S.inline.parentId).toBe(0)
  })

  it('doInlineReply posts to the parent post and closes the composer', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    const f = vi.fn(async () => new Response(JSON.stringify({ id: 3 })))
    vi.stubGlobal('fetch', f)
    S.inline.parentId = 0; S.inline.body = 'x'
    await S.doInlineReply()
    expect(f).not.toHaveBeenCalled()
    S.inline.parentId = 6; S.inline.body = 'hello'
    await S.doInlineReply()
    expect(f.mock.calls[0][0]).toBe('/api/posts/6/reply')
    expect(S.inline.parentId).toBe(0)
    expect(S.inline.body).toBe('')
    expect(S.notice).toBe('Replied')
  })

  it('doInlineReply keeps the draft when the server rejects it', async () => {
    const S = useStore()
    vi.spyOn(S, 'refresh').mockResolvedValue(undefined)
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response(JSON.stringify({ error: 'nope' }), { status: 400 })))
    S.inline.parentId = 6; S.inline.body = 'keep me'
    await S.doInlineReply()
    expect(S.inline.parentId).toBe(6)
    expect(S.inline.body).toBe('keep me')
  })

  it('thread navigation clears the inline composer', () => {
    const S = useStore()
    S.threadId = 1; S.openInline(4); S.inline.body = 'x'
    S.openThread(2)
    expect(S.inline.parentId).toBe(0)
    S.openInline(5); S.inline.body = 'y'
    S.applyRoute({ view: 'thread', threadId: 3 })
    expect(S.inline.parentId).toBe(0)
    expect(S.inline.body).toBe('')
  })
})
