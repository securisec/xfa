package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFindsAndScopes(t *testing.T) {
	s, b, a := seed(t)
	b2, _ := s.EnsureBoard("other", "")
	s.CreatePost(b.ID, a.Handle, "the sqlite driver needs WAL mode", "", nil)
	s.CreatePost(b2.ID, a.Handle, "sqlite is elsewhere", "", nil)
	dead, _ := s.CreatePost(b.ID, a.Handle, "sqlite tombstoned", "", nil)
	s.Tombstone(dead.ID, a.Handle)

	all, err := s.Search("sqlite", 0, 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("all-board search: %v len=%d want 2", err, len(all))
	}
	scoped, err := s.Search("sqlite", b.ID, 10)
	if err != nil || len(scoped) != 1 {
		t.Fatalf("scoped search: %v len=%d want 1", err, len(scoped))
	}
}

// A DB created before the FTS migration existed has posts but no posts_fts
// table; the external-content index starts empty, so migrateFTS must backfill
// it exactly once, on first creation.
func TestMigrateFTSBackfillsLegacyPosts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	open := func() *Store {
		t.Helper()
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return s
	}
	s := open()
	b, _ := s.EnsureBoard("test", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	if _, err := s.CreatePost(b.ID, a.Handle, "legacy chinchilla post", "", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	// Simulate the pre-FTS schema.
	if err := s.DB.Exec(`DROP TRIGGER posts_fts_ai; DROP TABLE posts_fts;`).Error; err != nil {
		t.Fatalf("drop fts: %v", err)
	}

	got, err := open().Search("chinchilla", 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("post-backfill search: %v len=%d want 1", err, len(got))
	}
	// Reopening again must not rebuild or duplicate-index anything.
	got, err = open().Search("chinchilla", 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("second reopen search: %v len=%d want 1", err, len(got))
	}
}

// The trigram tokenizer makes every query term a case-insensitive substring
// match: partial words must find posts that whole-token FTS would miss.
func TestSearchSubstringMatching(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "found a vulnerability in the parser", "", nil)
	s.CreatePost(b.ID, a.Handle, "unrelated chatter", "", nil)

	for _, q := range []string{"vuln", "nerab", "VULN"} {
		got, err := s.Search(q, 0, 10)
		if err != nil || len(got) != 1 {
			t.Fatalf("substring %q: %v len=%d want 1", q, err, len(got))
		}
	}
	none, err := s.Search("zzzqqq", 0, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("non-matching query: %v len=%d want 0", err, len(none))
	}
}

// Trigram MATCH terms under 3 characters return zero rows, so Search must
// route sub-3-rune queries through a LIKE scan — with %, _ and \ escaped so
// they match literally, never as wildcards.
func TestSearchShortQueryLikeFallback(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "found a vulnerability in the parser", "", nil)
	s.CreatePost(b.ID, a.Handle, "progress at 10% today", "", nil)
	s.CreatePost(b.ID, a.Handle, "opcode 0x99 decoded", "", nil)
	s.CreatePost(b.ID, a.Handle, "field a_b is snake_case", "", nil)

	got, err := s.Search("vu", 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("short query vu: %v len=%d want 1", err, len(got))
	}
	// "0%" must match the literal percent post only — an unescaped LIKE
	// pattern %0%% would also match "0x99".
	pct, err := s.Search("0%", 0, 10)
	if err != nil || len(pct) != 1 {
		t.Fatalf("short query 0%%: %v len=%d want 1", err, len(pct))
	}
	// "a_" must match the literal underscore post only — an unescaped _
	// wildcard would also match "parser" (contains "at"... any a<char>).
	und, err := s.Search("a_", 0, 10)
	if err != nil || len(und) != 1 {
		t.Fatalf("short query a_: %v len=%d want 1", err, len(und))
	}
	// A backslash in the query must be escaped too (likeEscaper's \ branch):
	// unescaped, a trailing \ would swallow the closing % of the pattern.
	s.CreatePost(b.ID, a.Handle, `dir a\b listed`, "", nil)
	bs, err := s.Search(`a\`, 0, 10)
	if err != nil || len(bs) != 1 {
		t.Fatalf("short query a\\: %v len=%d want 1", err, len(bs))
	}
	// 3 runes goes through MATCH; % is grammar-hostile so it lands in the
	// quoted-phrase retry and still matches the literal substring.
	full, err := s.Search("10%", 0, 10)
	if err != nil || len(full) != 1 {
		t.Fatalf("query 10%%: %v len=%d want 1", err, len(full))
	}
	// Board scoping applies on the LIKE path too.
	b2, _ := s.EnsureBoard("other", "")
	scoped, err := s.Search("vu", b2.ID, 10)
	if err != nil || len(scoped) != 0 {
		t.Fatalf("scoped short query: %v len=%d want 0", err, len(scoped))
	}
	// Tombstones are masked on the LIKE path too.
	dead, _ := s.CreatePost(b.ID, a.Handle, "vu appears here", "", nil)
	s.Tombstone(dead.ID, a.Handle)
	live, err := s.Search("vu", 0, 10)
	if err != nil || len(live) != 1 {
		t.Fatalf("tombstone-masked short query: %v len=%d want 1", err, len(live))
	}
	// A whitespace-only query must not degenerate into LIKE '%%' (match-all).
	blank, err := s.Search("   ", 0, 10)
	if err != nil || len(blank) != 0 {
		t.Fatalf("blank query: %v len=%d want 0", err, len(blank))
	}
}

// A DB created before the trigram switch has a posts_fts built with the
// default tokenizer; migrateFTS must detect it, drop the external-content
// index (loses nothing) and rebuild it with trigram.
func TestMigrateFTSUpgradesLegacyTokenizer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, _ := s.EnsureBoard("test", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	s.CreatePost(b.ID, a.Handle, "found a vulnerability in the parser", "", nil)
	s.CreatePost(b.ID, a.Handle, "second legacy post", "", nil)
	// Recreate posts_fts with the pre-trigram DDL (default tokenizer).
	preDDL := `DROP TABLE posts_fts;
		CREATE VIRTUAL TABLE posts_fts USING fts5(body, content='posts', content_rowid='id');
		INSERT INTO posts_fts(posts_fts) VALUES('rebuild');`
	if err := s.DB.Exec(preDDL).Error; err != nil {
		t.Fatalf("recreate legacy fts: %v", err)
	}
	if got, err := s.Search("vuln", 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("legacy index sanity: %v len=%d want 0 (substring must not match pre-migration)", err, len(got))
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen (migration): %v", err)
	}
	if got, err := s2.Search("vuln", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("post-migration substring search: %v len=%d want 1", err, len(got))
	}
	if got, err := s2.Search("vulnerability", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("post-migration whole-word search: %v len=%d want 1", err, len(got))
	}
	if got, err := s2.Search("legacy", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("post-migration second post: %v len=%d want 1", err, len(got))
	}
	var n int64
	if err := s2.DB.Raw(`SELECT count(*) FROM posts`).Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("post count after migration: %v n=%d want 2", err, n)
	}
}

// Reopening an already-trigram DB must not drop or rebuild. A drop+recreate
// leaves the sqlite_master DDL byte-identical, so the DDL check alone can't
// catch a rebuild-on-every-open regression — instead the index is desynced
// from posts before the reopen (a manual 'delete' command, same as the AD
// trigger issues): a rebuild would resurrect the deindexed post, so it must
// still be unfindable afterwards.
func TestMigrateFTSIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	ddl := func(s *Store) string {
		t.Helper()
		var sql string
		if err := s.DB.Raw(
			`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'posts_fts'`,
		).Scan(&sql).Error; err != nil {
			t.Fatalf("read fts DDL: %v", err)
		}
		return sql
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, _ := s.EnsureBoard("test", "")
	a, _ := s.RegisterAgent("claude", "sess", "")
	p, err := s.CreatePost(b.ID, a.Handle, "found a vulnerability in the parser", "", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	s.CreatePost(b.ID, a.Handle, "second post stays indexed", "", nil)
	first := ddl(s)
	if !strings.Contains(strings.ToLower(first), "trigram") {
		t.Fatalf("fresh DDL missing trigram: %q", first)
	}
	if err := s.DB.Exec(
		`INSERT INTO posts_fts(posts_fts, rowid, body) VALUES('delete', ?, ?)`,
		p.ID, p.Body,
	).Error; err != nil {
		t.Fatalf("desync index: %v", err)
	}
	if got, err := s.Search("vuln", 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("desync sanity: %v len=%d want 0", err, len(got))
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if second := ddl(s2); second != first {
		t.Fatalf("DDL changed across reopen:\n first=%q\nsecond=%q", first, second)
	}
	if got, err := s2.Search("vuln", 0, 10); err != nil || len(got) != 0 {
		t.Fatalf("deindexed post findable after reopen — a rebuild ran: %v len=%d want 0", err, len(got))
	}
	if got, err := s2.Search("indexed", 0, 10); err != nil || len(got) != 1 {
		t.Fatalf("post-reopen search: %v len=%d want 1", err, len(got))
	}
}

// Hyphenated tokens are everyday xfa queries (handles like `quiet-raven-59`,
// board slugs like `other-board`), but fts5 parses `a-b` as a column filter and
// fails with "no such column: b" — a plain SQL logic error, not a syntax error.
// isFTSParseErr must recognize it so the quoted-phrase retry fires.
func TestSearchHyphenatedQueryFallsBackToPhrase(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "ping @quiet-raven-59 about the WAL fix", "", nil)
	s.CreatePost(b.ID, a.Handle, "unrelated chatter", "", nil)

	got, err := s.Search("quiet-raven-59", 0, 10)
	if err != nil {
		t.Fatalf("hyphenated query errored: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("hyphenated query: len=%d want 1 (%+v)", len(got), got)
	}
	// A hyphenated query matching nothing must still be a clean empty result.
	none, err := s.Search("other-board", 0, 10)
	if err != nil {
		t.Fatalf("non-matching hyphenated query errored: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("non-matching hyphenated query: len=%d want 0", len(none))
	}
}

// Ordinary punctuation must not surface raw FTS5 grammar errors: on a parse
// error, Search retries the query as a quoted phrase. Real operators still
// work when the raw query parses.
func TestSearchPunctuationFallback(t *testing.T) {
	s, b, a := seed(t)
	s.CreatePost(b.ID, a.Handle, "hey what's up world", "", nil)
	s.CreatePost(b.ID, a.Handle, "posts about sqlite", "", nil)
	s.CreatePost(b.ID, a.Handle, "posts about golang", "", nil)

	got, err := s.Search("what's up", 0, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("punctuation query: %v len=%d want 1", err, len(got))
	}
	both, err := s.Search("sqlite OR golang", 0, 10)
	if err != nil || len(both) != 2 {
		t.Fatalf("OR query: %v len=%d want 2", err, len(both))
	}
	// Unbalanced quotes are sanitized by the quoted-phrase retry (doubling
	// internal quotes always yields a valid phrase): no raw SQLite error, no
	// spurious matches. The friendly-error path guards retry-time failures.
	none, err := s.Search(`un"balanced`, 0, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("garbage query: err=%v len=%d want 0", err, len(none))
	}
}
