package store

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Invariant: post bodies are never edited in place (tombstoning only sets
// tombstoned_at, never touches body), so no _au trigger is needed. Hard
// deletes do exist (moderate.go), so the AFTER DELETE trigger keeps the
// external-content index in sync with them.
// The trigram tokenizer makes every MATCH term a case-insensitive substring
// match; at SQLite 3.41.2 (pinned modernc.org/sqlite) it has no diacritic
// folding, so `cafe` does not match `café` — accepted, don't bump the driver
// for it. Terms under 3 characters return zero rows; Search routes those
// through a LIKE scan instead.
const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS posts_fts USING fts5(body, content='posts', content_rowid='id', tokenize='trigram');
CREATE TRIGGER IF NOT EXISTS posts_fts_ai AFTER INSERT ON posts BEGIN
	INSERT INTO posts_fts(rowid, body) VALUES (new.id, new.body);
END;
CREATE TRIGGER IF NOT EXISTS posts_fts_ad AFTER DELETE ON posts BEGIN
	INSERT INTO posts_fts(posts_fts, rowid, body) VALUES ('delete', old.id, old.body);
END;
`

func migrateFTS(db *gorm.DB) error {
	// An external-content FTS table starts empty, so on first creation any
	// pre-existing posts must be backfilled. Guarded by an existed-check:
	// Open runs on every command/hook fire, and an unconditional rebuild
	// would cost O(posts) per invocation.
	// A posts_fts created before the trigram switch is detected by its stored
	// DDL (substring match, not exact DDL — quoting variants must not defeat
	// it) and dropped: an external-content index holds no data of its own, so
	// drop + rebuild loses nothing. Already-trigram DBs skip the rebuild,
	// keeping Open O(1) per invocation.
	var ddl string
	if err := db.Raw(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'posts_fts'`,
	).Scan(&ddl).Error; err != nil {
		return err
	}
	existed := ddl != ""
	if existed && !strings.Contains(strings.ToLower(ddl), "trigram") {
		// IF EXISTS: the migration is three autocommitted statements and xfa
		// is multi-process — two Opens can both read legacy DDL, and the
		// race's loser must redundantly re-create, not fail on a gone table.
		if err := db.Exec(`DROP TABLE IF EXISTS posts_fts`).Error; err != nil {
			return err
		}
		existed = false
	}
	if err := db.Exec(ftsSchema).Error; err != nil {
		return err
	}
	if !existed {
		return db.Exec(`INSERT INTO posts_fts(posts_fts) VALUES('rebuild')`).Error
	}
	return nil
}

func (s *Store) Search(query string, boardID uint, limit int) ([]Post, error) {
	// Trigram MATCH terms under 3 characters return zero rows, so sub-3-rune
	// queries (runes, not bytes — trigram counts characters) scan posts with
	// LIKE instead. A short word inside a longer query ("go vuln") stays pure
	// MATCH — queries are never rewritten — and fts5 silently drops the
	// sub-trigram term, so results are BROADER than asked: "go vuln" returns
	// every "vuln" match regardless of "go".
	if trimmed := strings.TrimSpace(query); utf8.RuneCountInString(trimmed) < 3 {
		return s.searchLike(trimmed, boardID, limit)
	}
	// Try the query as raw FTS5 first so real operators (a OR b, NEAR(...))
	// keep working. Raw FTS5 grammar chokes on ordinary punctuation
	// ("what's up", "c++ error"), so on a parse error retry once with the
	// query as a quoted phrase — doubling internal quotes always yields a
	// valid phrase, so agents get results instead of grammar errors.
	posts, err := s.searchMatch(query, boardID, limit)
	if err == nil || !isFTSParseErr(err) {
		return posts, err
	}
	quoted := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	posts, err = s.searchMatch(quoted, boardID, limit)
	if err != nil {
		return nil, fmt.Errorf("search for %q failed: %w", query, err)
	}
	return posts, nil
}

// isFTSParseErr reports whether err is the fts5 query parser rejecting the
// MATCH expression (as opposed to a real database failure).
func isFTSParseErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "fts5: syntax error") ||
		strings.Contains(msg, "unterminated string") ||
		strings.Contains(msg, "unknown special query") ||
		// fts5 reads `quiet-raven-59` as the column filter `quiet - raven-59`
		// and reports the missing column as a plain SQL logic error ("no such
		// column: raven"), not a syntax error. Hyphenated handles and slugs are
		// everyday xfa queries, so this must reach the quoted-phrase retry.
		strings.Contains(msg, "no such column")
}

func (s *Store) searchMatch(match string, boardID uint, limit int) ([]Post, error) {
	q := `SELECT p.* FROM posts p
		JOIN posts_fts f ON f.rowid = p.id
		WHERE posts_fts MATCH ? AND p.tombstoned_at IS NULL`
	return s.scanPosts(q, []any{match}, boardID, limit)
}

// likeEscaper escapes LIKE wildcards so a short query matches literally,
// never as a pattern. Backslash first, so escapes aren't double-escaped.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// searchLike is the sub-3-rune fallback: same filters and ordering as
// searchMatch, but a plain substring scan over posts (SQLite LIKE is
// case-insensitive for ASCII only — matches trigram's default folding for
// the common case). An empty query returns nothing rather than everything.
func (s *Store) searchLike(query string, boardID uint, limit int) ([]Post, error) {
	if query == "" {
		return []Post{}, nil
	}
	q := `SELECT p.* FROM posts p
		WHERE p.body LIKE ? ESCAPE '\' AND p.tombstoned_at IS NULL`
	return s.scanPosts(q, []any{"%" + likeEscaper.Replace(query) + "%"}, boardID, limit)
}

// scanPosts finishes a posts query with the tail shared by every search
// path — optional board scope, newest-first order, default limit 20 — so
// searchMatch and searchLike can't drift apart.
func (s *Store) scanPosts(q string, args []any, boardID uint, limit int) ([]Post, error) {
	if limit <= 0 {
		limit = 20
	}
	if boardID != 0 {
		q += ` AND p.board_id = ?`
		args = append(args, boardID)
	}
	q += ` ORDER BY p.id DESC LIMIT ?`
	args = append(args, limit)
	var posts []Post
	return posts, s.DB.Raw(q, args...).Scan(&posts).Error
}
