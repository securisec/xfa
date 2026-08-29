// The shared store, ported from the pre-Vite embedded UI's Alpine.js app()
// object (internal/web/static/index.html, old lines 707–1268). State fields,
// method names and logic are unchanged; the pure formatters moved to
// lib/format.js and the transport to lib/api.js, so `this.slugOf(id)` became
// `slugOf(id, this.boards)` and `this.get(url)` became `get(url)`. The
// long-body clipping helpers (bodyOverflow/bodyText/expandLabel/isExpanded/
// toggleExpand/dblToggleExpand) live in the post component now; the
// `expanded` set they key off stays here, because routing clears it.

import { reactive } from 'vue'
import { get, send as apiSend } from './lib/api.js'
import { slugOf } from './lib/format.js'

let store = null

// Every delete in the web UI is the moderator hard delete, not the CLI's
// tombstone; the dialog says so once, under the blast-radius line.
const HARD_DELETE_HINT = 'hard delete — the whole reply subtree goes, no tombstone.'

// One store for the whole app, created on first use: the old page had exactly
// one app() object and every pane read the same fields off it.
export function useStore() {
  if (!store) store = reactive(createStore())
  return store
}

function createStore() {
  return {
    me: {handle: '', board: ''},
    boards: [],
    view: 'threads',        // threads | thread | search | questions | inbox | stats
    board: '',              // current slug; '' = all-boards overview
    threads: [], thread: [], results: [], questions: [], inbox: [], stats: null,
    threadId: 0,
    // Session filter for the thread list. sessions is the current board's
    // picker rows from /api/sessions; session is the selected id, '' meaning
    // "all sessions" — the default, which runs the unfiltered endpoint.
    sessions: [], session: '',
    rename: {open: false, session: '', name: ''},
    // One confirmation dialog for the whole app (ConfirmModal.vue), replacing
    // window.confirm. confirm() opens it and returns a promise; answerDialog
    // settles it. resolve is null while closed.
    dialog: {open: false, title: '', body: '', hint: '', action: 'delete', danger: true, resolve: null},
    // ids of thread-view posts the human expanded past the clip. Keyed by post
    // id, so a 5s poll re-rendering the same thread keeps them open; cleared
    // only when the thread view moves to a different thread.
    expanded: {},
    q: '', composer: {open: false, board: '', body: '', tag: ''}, reply: {body: ''},
    inline: {parentId: 0, body: ''},
    error: '', notice: '', timer: null, errTimer: null, okTimer: null,
    sending: false, busy: false, seq: 0,

    async init() {
      try {
        this.me = await get('/api/me');
      } catch (e) { this.fail(e); }
      // The URL wins on load; /api/me's board is only the fallback for a bare
      // URL, so a shared or reloaded deep link lands where it says it does.
      const r = this.parseHash(location.hash);
      if (r) this.applyRoute(r);
      else this.board = this.me.board || '';
      this.syncHash();
      window.addEventListener('hashchange', () => this.onHashChange());
      await this.loadBoards();
      await this.refresh(true);
      // no teardown: matches the old page, which lived until unload
      this.timer = setInterval(() => { if (!document.hidden) this.refresh(); }, 5000);
    },

    // ── hash routing ───────────────────────────────────────────────────
    // Routes: #/ · #/b/<slug> · #/t/<id> · #/search/<q> · #/questions ·
    // #/inbox · #/stats. Navigators write the hash; a single hashchange
    // listener reads it back, so Back/Forward are just another writer and
    // there is exactly one path from URL into state.
    //
    // routeHash formats the current state; the loop guard is the identity
    // "if the hash already says what the state says, do nothing" — applied
    // on both the write side (syncHash) and the read side (onHashChange).
    routeHash() {
      if (this.view === 'thread')    return '#/t/' + this.threadId;
      if (this.view === 'search')    return '#/search/' + encodeURIComponent(this.q);
      if (this.view === 'questions') return '#/questions';
      if (this.view === 'inbox')     return '#/inbox';
      if (this.view === 'stats')     return '#/stats';
      return this.board ? '#/b/' + encodeURIComponent(this.board) : '#/';
    },
    // parseHash returns a route object, or null for "no route here" (a bare
    // URL, or something we don't recognise) — null means: leave state alone.
    parseHash(h) {
      let raw = String(h || '');
      if (raw.charAt(0) === '#') raw = raw.slice(1);
      if (raw === '') return null;              // bare URL: fall back to /api/me
      if (raw === '/') return {view: 'threads', board: ''};
      if (raw.charAt(0) === '/') raw = raw.slice(1);
      const cut = raw.indexOf('/');
      const head = cut < 0 ? raw : raw.slice(0, cut);
      const rest = cut < 0 ? '' : raw.slice(cut + 1);
      const dec = (s) => { try { return decodeURIComponent(s); } catch (e) { return s; } };
      if (head === 'b' && rest) return {view: 'threads', board: dec(rest)};
      if (head === 't' && /^[0-9]+$/.test(rest)) return {view: 'thread', threadId: parseInt(rest, 10)};
      if (head === 'search') return {view: 'search', q: dec(rest)};
      if (head === 'questions' && !rest) return {view: 'questions'};
      if (head === 'inbox' && !rest) return {view: 'inbox'};
      if (head === 'stats' && !rest) return {view: 'stats'};
      return null;
    },
    applyRoute(r) {
      if (r.view === 'thread') {
        // A different thread means the old payload is stale, the in-progress
        // reply belongs to the post we just left, and the expanded-post set
        // describes posts we are navigating away from.
        if (this.threadId !== r.threadId) { this.thread = []; this.reply.body = ''; this.expanded = {}; this.closeInline(); }
        this.threadId = r.threadId;
      }
      // A different board has a different session list, so a filter picked on
      // the old one would name a session that board has never heard of.
      if (r.board !== undefined) {
        if (r.board !== this.board) this.session = '';
        this.board = r.board;
      }
      if (r.q !== undefined) this.q = r.q;
      this.view = r.view;
    },
    syncHash() {
      const h = this.routeHash();
      if (location.hash === h) return;   // loop guard: no redundant history entry
      location.hash = h;
    },
    onHashChange() {
      const r = this.parseHash(location.hash);
      if (!r) return;                              // unknown route: ignore
      if (this.routeHash() === location.hash) return;  // loop guard: already there
      this.applyRoute(r);
      this.refresh(true);
    },

    // ── transport ──────────────────────────────────────────────────────
    // get/parse moved to lib/api.js; send stays here because a write's
    // follow-up refresh and its toast are store concerns, not transport.
    async send(method, url, body) {
      try {
        const j = await apiSend(method, url, body);
        await this.refresh(true);
        return j;
      } catch (e) { this.fail(e); throw e; }
    },
    fail(e) {
      this.error = (e && e.message) ? e.message : String(e);
      clearTimeout(this.errTimer);
      this.errTimer = setTimeout(() => { this.error = ''; }, 5000);
    },
    // confirm asks the human a yes/no question through the ConfirmModal and
    // resolves true only when they press the action button. Escape, the
    // backdrop and cancel all resolve false. `danger` picks the red button.
    confirm({title, body, hint = '', action = 'delete', danger = true}) {
      return new Promise(resolve => {
        this.dialog = {open: true, title, body, hint, action, danger, resolve};
      });
    },
    answerDialog(ok) {
      const resolve = this.dialog.resolve;
      this.dialog.open = false;
      this.dialog.resolve = null;
      if (resolve) resolve(ok);
    },
    ok(msg) {
      this.notice = msg;
      clearTimeout(this.okTimer);
      this.okTimer = setTimeout(() => { this.notice = ''; }, 2500);
    },

    // ── data loading ───────────────────────────────────────────────────
    async loadBoards() {
      try { this.boards = await get('/api/boards'); } catch (e) { this.fail(e); }
    },
    boardQuery(sep) {
      return this.board ? sep + 'board=' + encodeURIComponent(this.board) : '';
    },
    // The unfiltered default emits no param at all: an empty ?session= is the
    // same as absent to the API, but not sending it keeps the two states
    // visibly distinct in the network log.
    sessionQuery(sep) {
      return this.session ? sep + 'session=' + encodeURIComponent(this.session) : '';
    },
    // refresh reloads only the active view's data. Every failure lands in
    // the toast; a poll that fails must never tear the page down.
    //
    // force=true is for anything the human just did (navigation, a write's
    // follow-up load): it must never be dropped, or a click landing mid-poll
    // would switch the view and leave the pane empty until the next tick.
    // Only the 5s interval poll defers to an in-flight load.
    //
    // Overlap is made safe by a sequence number rather than by a lock: each
    // run claims a ticket, and a run whose ticket has been superseded discards
    // its response instead of writing it, so a slow earlier fetch can never
    // land on top of a newer one (nor raise a stale toast).
    async refresh(force) {
      if (this.busy && !force) return;
      const my = ++this.seq;
      const mine = () => my === this.seq;
      this.busy = true;
      try {
        const boards = await get('/api/boards');
        if (!mine()) return;
        this.boards = boards;
        if (this.view === 'threads') {
          const v = this.board
            ? await get('/api/boards/' + encodeURIComponent(this.board) + '/threads'
                        + this.sessionQuery('?'))
            : [];
          if (mine()) this.threads = v;
        } else if (this.view === 'thread') {
          if (this.threadId) {
            const v = await get('/api/threads/' + this.threadId);
            if (mine()) this.thread = v;
          }
        } else if (this.view === 'search') {
          const v = this.q.trim()
            ? await get('/api/search?q=' + encodeURIComponent(this.q) + this.boardQuery('&'))
            : [];
          if (mine()) this.results = v;
        } else if (this.view === 'questions') {
          const v = await get('/api/questions' + this.boardQuery('?'));
          if (mine()) this.questions = v;
        } else if (this.view === 'inbox') {
          const v = await get('/api/inbox');
          if (mine()) this.inbox = v;
        } else if (this.view === 'stats') {
          const v = await get('/api/stats' + this.boardQuery('?'));
          if (mine()) this.stats = v;
        }
        // Session rows come last, for whichever board is selected in any view:
        // the sidebar nests them under the active board regardless of view, so
        // they load whenever a board is active. They come last because the
        // view's own data is what the human is waiting on and a bad board slug
        // should toast about the board, not about a sessions call that happened
        // to fire first. With no board there is nothing to filter, so the list
        // is cleared.
        if (this.board) {
          const list = await get('/api/sessions' + this.boardQuery('?'));
          if (mine()) { this.sessions = list; this.pruneSession(); }
        } else if (mine() && (this.sessions.length || this.session)) {
          this.sessions = []; this.session = '';   // all-boards view: nothing to filter
        }
      } catch (e) {
        if (mine()) this.fail(e);
      } finally {
        // Only the newest run owns the flag; an older one clearing it would
        // reopen the poll while a load is still running.
        if (mine()) this.busy = false;
      }
    },

    // ── navigation ─────────────────────────────────────────────────────
    // Every navigator forces the load: the human is waiting on it, so it must
    // not be swallowed by a poll that happens to be in flight. Each also
    // writes the hash, so Back/Forward retrace the same steps.
    go(view) { this.view = view; this.syncHash(); this.refresh(true); },
    // Switching boards drops the session filter (see applyRoute) — the list it
    // was picked from belongs to the board being left.
    pickBoard(slug) {
      if (slug !== this.board) { this.session = ''; this.sessions = []; }
      this.board = slug; this.view = 'threads'; this.syncHash(); this.refresh(true);
    },
    runSearch() { this.view = 'search'; this.syncHash(); this.refresh(true); },
    openThread(id) {
      if (this.threadId !== id) this.expanded = {};   // see applyRoute
      this.threadId = id; this.thread = []; this.reply.body = ''; this.closeInline();
      this.view = 'thread'; this.syncHash(); this.refresh(true);
    },
    // A post reached from search or the inbox may be a reply, and
    // /api/threads/{id} only accepts roots. Resolve the root through the
    // board grouping (roots come first in each group) before navigating.
    async openPost(p) {
      if (!p.parent_id) { this.openThread(p.id); return; }
      const slug = slugOf(p.board_id, this.boards);
      // Unreachable: slugOf() answers '?' for an unknown id. Kept for parity;
      // the '?' lookup 404s and falls through below.
      if (!slug) { this.openThread(p.parent_id); return; }
      try {
        const groups = await get('/api/boards/' + encodeURIComponent(slug) + '/board');
        for (const g of groups) {
          if (g.length && g.some(x => x.id === p.id)) { this.openThread(g[0].id); return; }
        }
      } catch (e) { this.fail(e); }
      this.openThread(p.parent_id);
    },
    // openRef is the single entry point for every "#<id> reference" click —
    // body anchors (PostBody) and backlink chips (ThreadDetail) alike.
    //
    // knownRoot is the thread_id off a recorded post_links row. When present
    // it is authoritative and cheap, and it is correct even when the target
    // lives on another board.
    //
    // Without one, the id still identifies a post globally: post_links rows
    // only exist for refs written after the cross-link feature shipped, so the
    // vast majority of real #id references have no row at all and would
    // otherwise be dead text. So the bare id is resolved instead — first
    // against whatever is already loaded (which lets openPost walk a reply up
    // to its root), then, for the common case of an id nobody has loaded, by
    // asking the board's grouping to locate it. /api/threads/{id} only accepts
    // ROOTS, so a bare reply id would 404 there; the grouping maps a reply to
    // its root the same way openPost does. Only if that cannot place the id
    // (cross-board ref we can't resolve front-end-only, or a stale/deleted id)
    // is it opened optimistically as a root — correct for a cross-board root
    // via the board-agnostic thread endpoint, and an honest error toast for a
    // genuinely unresolvable id.
    async openRef(id, knownRoot) {
      if (Number.isFinite(knownRoot) && knownRoot > 0) { this.openThread(knownRoot); return; }
      const n = Number(id);
      if (!Number.isFinite(n) || n <= 0) return;   // not an id: nothing to open
      const loaded = [
        ...(this.thread || []),
        ...(this.threads || []).map(t => t && t.root).filter(Boolean),
        ...(this.results || []),
        ...(this.questions || []),
        ...(this.inbox || []),
      ];
      const found = loaded.find(p => p && p.id === n);
      if (found) { await this.openPost(found); return; }
      // Not loaded: try to place the id in the board's grouping, so a bare
      // reply id resolves to its root instead of 404ing the thread endpoint.
      // The slug to search is the open thread's own board, falling back to the
      // active board; with neither there is nothing to query.
      const root = this.threadRoot();
      const slug = root ? slugOf(root.board_id, this.boards) : (this.board || '');
      if (slug && slug !== '?') {
        try {
          const groups = await get('/api/boards/' + encodeURIComponent(slug) + '/board');
          for (const g of groups) {
            if (g.length && g.some(x => x.id === n)) { this.openThread(g[0].id); return; }
          }
        } catch (e) { /* fall through to the optimistic open below */ }
      }
      this.openThread(n);
    },

    // ── helpers ────────────────────────────────────────────────────────
    threadRoot() {
      return (this.thread || []).find(p => !p.parent_id) || null;
    },
    // threadRows flattens the thread into render order with an indent depth.
    // The API does not promise parent-before-child ordering, so the tree is
    // rebuilt from parent_id chains: replies whose parent is missing from the
    // payload still render (at depth 1), and a cycle cannot loop forever.
    threadRows() {
      const posts = this.thread || [];
      const byId = new Map(posts.map(p => [p.id, p]));
      const kids = new Map();
      const orphans = [];
      const roots = [];
      for (const p of posts) {
        if (!p.parent_id) { roots.push(p); continue; }
        if (byId.has(p.parent_id)) {
          if (!kids.has(p.parent_id)) kids.set(p.parent_id, []);
          kids.get(p.parent_id).push(p);
        } else {
          orphans.push(p);
        }
      }
      const byTime = (a, b) => (a.created_at < b.created_at ? -1
                             : a.created_at > b.created_at ? 1 : a.id - b.id);
      const rows = [], seen = new Set();
      const walk = (p, depth) => {
        if (seen.has(p.id)) return;   // cycle guard
        seen.add(p.id);
        rows.push({post: p, depth: depth});
        (kids.get(p.id) || []).sort(byTime).forEach(c => walk(c, depth + 1));
      };
      roots.sort(byTime).forEach(r => walk(r, 0));
      orphans.sort(byTime).forEach(o => { if (!seen.has(o.id)) { seen.add(o.id); rows.push({post: o, depth: 1}); } });
      posts.forEach(p => { if (!seen.has(p.id)) { seen.add(p.id); rows.push({post: p, depth: 1}); } });
      return rows;
    },
    canResolve(p) {
      // Questions (top-level) as before; human-authored posts at any depth, any
      // tag — the human's own close button for requests and "thanks" replies.
      if (p.resolved_at || p.deleted) return false;
      return (p.tag === 'question' && !p.parent_id) || !!p.human;
    },

    // ── session filter ─────────────────────────────────────────────────
    // The picker only ever narrows the thread list; it never touches read
    // cursors (nothing in this UI does) and '' always means the untouched,
    // unfiltered view.
    // Picking a session (from the sidebar or the mobile dropdown) always lands
    // on that board's thread list — the filter only has meaning there, so a
    // pick made from another view switches to where the human can see it work.
    pickSession(id) {
      this.session = id || '';
      this.view = 'threads';
      this.syncHash();
      this.refresh(true);
    },
    // A filter aimed at a session with nothing on this board would render an
    // empty list with no explanation, so a vanished selection falls back to
    // all-sessions rather than sticking.
    pruneSession() {
      if (this.session && !this.sessions.some(s => s.session_id === this.session)) {
        this.session = '';
      }
    },
    sessionRow(id) { return this.sessions.find(s => s.session_id === id) || null; },
    sessionOn(p) {
      return p && this.session && p.session_id === this.session ? 'sesspill-on' : '';
    },
    openRename() {
      if (!this.session) return;
      const row = this.sessionRow(this.session);
      this.rename.session = this.session;
      // Seed with the stored name, never the fallback label: a fallback is a
      // placeholder, and pre-filling it would make "save" adopt it as a name.
      this.rename.name = (row && row.name) || '';
      this.rename.open = true;
    },
    async doRename() {
      const name = this.rename.name.trim();
      if (this.sending || !this.rename.session || !name) return;
      this.sending = true;
      try {
        // send() reloads afterwards, and the reload re-reads /api/sessions —
        // the authority on what a session is now called.
        await this.send('POST',
          '/api/sessions/' + encodeURIComponent(this.rename.session) + '/name', {name: name});
        this.rename.open = false;
        this.ok('Renamed');
      } catch (e) { /* toast already raised; keep the draft */ }
      finally { this.sending = false; }
    },

    // ── writes ─────────────────────────────────────────────────────────
    openComposer() {
      this.composer.board = this.board || (this.boards[0] ? this.boards[0].slug : '');
      this.composer.open = true;
    },
    async doPost() {
      if (this.sending) return;
      this.sending = true;
      try {
        await this.send('POST', '/api/posts', {
          board: this.composer.board,
          body: this.composer.body,
          tag: this.composer.tag.trim(),
        });
        this.composer.open = false;
        this.composer.body = ''; this.composer.tag = '';
        this.ok('Posted');
      } catch (e) { /* toast already raised; keep the draft */ }
      finally { this.sending = false; }
    },
    async doReply() {
      if (this.sending || !this.threadId) return;
      this.sending = true;
      try {
        await this.send('POST', '/api/posts/' + this.threadId + '/reply', {body: this.reply.body});
        this.reply.body = '';
        this.ok('Replied');
      } catch (e) { /* toast already raised; keep the draft */ }
      finally { this.sending = false; }
    },
    // Per-post composer in the thread view. Exactly one may be open;
    // opening another post's composer replaces it and drops the draft —
    // the draft belonged to the post it was written under.
    openInline(id) {
      if (this.inline.parentId === id) { this.closeInline(); return; }
      this.inline.parentId = id; this.inline.body = '';
    },
    closeInline() { this.inline.parentId = 0; this.inline.body = ''; },
    async doInlineReply() {
      if (this.sending || !this.inline.parentId) return;
      this.sending = true;
      try {
        await this.send('POST', '/api/posts/' + this.inline.parentId + '/reply', {body: this.inline.body});
        this.closeInline();
        this.ok('Replied');
      } catch (e) { /* toast already raised; keep the draft */ }
      finally { this.sending = false; }
    },
    async doResolve(id) {
      try { await this.send('POST', '/api/posts/' + id + '/resolve', null); } catch (e) {}
    },

    // ── delete ─────────────────────────────────────────────────────────
    // descendantCount walks the currently loaded thread's parent_id chains
    // to count everything under `id` — children, grandchildren, and so on.
    // Only meaningful while that thread is loaded; the threads list uses
    // its own already-summarized reply count instead (see doDeleteThread).
    // The API does not promise a cycle-free parent_id graph, so this uses
    // the same seen-set cycle guard as threadRows().
    descendantCount(id) {
      const posts = this.thread || [];
      const kids = new Map();
      for (const p of posts) {
        if (!p.parent_id) continue;
        if (!kids.has(p.parent_id)) kids.set(p.parent_id, []);
        kids.get(p.parent_id).push(p);
      }
      let n = 0;
      const seen = new Set();
      const walk = (pid) => {
        for (const c of (kids.get(pid) || [])) {
          if (seen.has(c.id)) continue;   // cycle guard
          seen.add(c.id);
          n++;
          walk(c.id);
        }
      };
      walk(id);
      return n;
    },
    // deleteBlastMessage states the blast radius before a hard delete
    // fires: a leaf gets the short form, anything with descendants names
    // the count, and a thread root is phrased as "thread" rather than
    // "post" so the human knows the whole thread is going.
    deleteBlastMessage(kind, n) {
      if (n <= 0) return 'Delete this ' + kind + '?';
      return 'Delete this ' + kind + ' and its ' + n + ' ' + (n === 1 ? 'reply' : 'replies') + '?';
    },
    // doDeletePost fires from the thread view, where a post's descendants
    // are already loaded (descendantCount). A root post is phrased as the
    // whole thread. Deleting the thread currently open switches back to
    // the thread list before the request lands, so the write's automatic
    // refresh loads the list instead of re-fetching the now-404ing thread.
    // If the request fails (404/500), that pre-emptive navigation is undone
    // in the catch — a failed delete must not eject the human from the
    // thread they were looking at.
    async doDeletePost(post) {
      const isRoot = !post.parent_id;
      const kind = isRoot ? 'thread' : 'post';
      const msg = this.deleteBlastMessage(kind, this.descendantCount(post.id));
      if (!await this.confirm({title: 'delete ' + kind, body: msg, hint: HARD_DELETE_HINT})) return;
      const closingThread = this.view === 'thread' && isRoot && post.id === this.threadId;
      if (closingThread) { this.thread = []; this.view = 'threads'; this.syncHash(); }
      try {
        const j = await this.send('DELETE', '/api/posts/' + post.id, null);
        this.ok('Deleted ' + (j && typeof j.deleted === 'number' ? j.deleted : 1) + ' post(s)');
      } catch (e) {
        // toast already raised by send(); restore the thread view we
        // pre-emptively left, since the delete never actually happened.
        if (closingThread) { this.view = 'thread'; this.syncHash(); await this.refresh(true); }
      }
    },
    // doDeleteThread fires from a thread-list card, where the reply count
    // is already the summary's — no need to load the thread just to count it.
    async doDeleteThread(t) {
      const msg = this.deleteBlastMessage('thread', t.replies || 0);
      if (!await this.confirm({title: 'delete thread', body: msg, hint: HARD_DELETE_HINT})) return;
      try {
        const j = await this.send('DELETE', '/api/posts/' + t.root.id, null);
        this.ok('Deleted ' + (j && typeof j.deleted === 'number' ? j.deleted : 1) + ' post(s)');
      } catch (e) { /* toast already raised */ }
    },
    // doDeleteSession forgets a session and every post it authored. If it
    // is the active filter, the filter is cleared first so the write's
    // automatic refresh re-queries unfiltered instead of a session that no
    // longer has anything to match.
    async doDeleteSession(s) {
      const label = s.display_name || s.session_id;
      if (!await this.confirm({title: 'delete session', body: 'Delete session ' + label + ' and all its posts?', hint: HARD_DELETE_HINT})) return;
      if (this.session === s.session_id) this.session = '';
      try {
        const j = await this.send('DELETE', '/api/sessions/' + encodeURIComponent(s.session_id), null);
        this.ok('Deleted ' + (j && typeof j.deleted === 'number' ? j.deleted : 0) + ' post(s)');
      } catch (e) { /* toast already raised */ }
    },
    // The mobile session bar shows only the picked session's id, not the
    // full row, so its delete button resolves the row from `sessions` — the
    // same lookup the rename affordance already relies on.
    deleteSelectedSession() {
      const row = this.sessionRow(this.session);
      if (row) this.doDeleteSession(row);
    },
  }
}
