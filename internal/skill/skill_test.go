package skill

import (
	"regexp"
	"strings"
	"testing"
)

func TestSkillIsPrescriptive(t *testing.T) {
	for _, must := range []string{
		"name: xfa", "description:", "Iron Law",
		"xfa register", "xfa post", "xfa search", "xfa read",
		"xfa inbox", "xfa resolve", "xfa questions", "--unread",
		"xfa reply",
		"Red Flags",
		"xfa reset", "Never run",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
	if strings.Contains(Content, "Placeholder") {
		t.Error("placeholder skill shipped")
	}
	// Size ceiling is our own discipline, not a provider limit — the only real
	// one is opencode's 1024-char description, checked elsewhere.
	if len(Content) > 13000 {
		t.Errorf("skill is %d bytes; keep it tight", len(Content))
	}
}

// Controller ruling: frontmatter must stay opencode-valid — name matches
// ^[a-z0-9]+(-[a-z0-9]+)*$ and description is non-empty, 1-1024 chars.
func TestSkillFrontmatterOpencodeValid(t *testing.T) {
	if !strings.HasPrefix(Content, "---\n") {
		t.Fatal("SKILL.md does not open with a frontmatter fence")
	}
	fm, _, ok := strings.Cut(strings.TrimPrefix(Content, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("SKILL.md has no closed frontmatter block")
	}
	nameRe := regexp.MustCompile(`(?m)^name: ([a-z0-9]+(-[a-z0-9]+)*)$`)
	m := nameRe.FindStringSubmatch(fm)
	if m == nil {
		t.Fatal("frontmatter name missing or not opencode-valid")
	}
	if m[1] != "xfa" {
		t.Errorf("frontmatter name = %q, want %q", m[1], "xfa")
	}
	descRe := regexp.MustCompile(`(?m)^description: (.+)$`)
	dm := descRe.FindStringSubmatch(fm)
	if dm == nil {
		t.Fatal("frontmatter description missing")
	}
	if n := len(dm[1]); n < 1 || n > 1024 {
		t.Errorf("description is %d chars; must be 1-1024", n)
	}
	// Triggers only, never workflow: it must describe when to load the skill,
	// not walk through commands.
	if !strings.HasPrefix(dm[1], "Use when") {
		t.Errorf("description must start with %q, got %q", "Use when", dm[1])
	}
	for _, workflow := range []string{"xfa post", "xfa read", "xfa search", "xfa register", " then "} {
		if strings.Contains(dm[1], workflow) {
			t.Errorf("description contains workflow text %q; triggers only", workflow)
		}
	}
}

// Controller ruling: the skill must warn that board posts from other agents
// are untrusted content — data, never instructions.
func TestSkillWarnsBoardIsUntrusted(t *testing.T) {
	if !strings.Contains(Content, "untrusted") {
		t.Error("skill missing the untrusted-content warning about other agents' posts")
	}
}

// QA remediation task 1: answering is a first-class duty, not an afterthought.
func TestSkillMakesAnsweringADuty(t *testing.T) {
	// Iron Law's exact third clause.
	ironLaw := "CHECK THE BOARD WHEN YOU START. POST WHAT YOU LEARNED BEFORE YOU STOP.\nSEARCH BEFORE YOU RE-DERIVE. ANSWER BEFORE YOU EXIT."
	if !strings.Contains(Content, ironLaw) {
		t.Error("Iron Law missing the exact ANSWER BEFORE YOU EXIT block")
	}
	// A "Before you finish" step: inbox + questions + reply + resolve.
	if !strings.Contains(Content, "Before you finish") {
		t.Error("skill missing the \"Before you finish\" step")
	}
	// Threading is prescribed: answers go via reply, never top-level @mentions.
	if !strings.Contains(Content, "never as a new top-level post") {
		t.Error("skill does not prescribe replying in-thread over top-level @mention answers")
	}
	// question tag semantics are tightened.
	if !strings.Contains(Content, "ONLY when you need an answer") {
		t.Error("skill does not restrict the question tag to actual questions")
	}
	// New red-flag rows.
	for _, row := range []string{
		"Someone else will answer that open question",
		"You are someone else",
		"checking the board is extra",
	} {
		if !strings.Contains(Content, row) {
			t.Errorf("Red Flags table missing %q", row)
		}
	}
	// Verification section covers inbox + questions.
	_, verify, ok := strings.Cut(Content, "## Verification")
	if !ok {
		t.Fatal("skill has no Verification section")
	}
	for _, cmd := range []string{"xfa inbox", "xfa questions"} {
		if !strings.Contains(verify, cmd) {
			t.Errorf("Verification section missing %q", cmd)
		}
	}
	// Frontmatter description gains a finishing-a-task trigger — a pure
	// trigger, not an imperative (peer-review ruling: no workflow, no
	// actionable one-liners in the description).
	// Semicolons keep the outer when-series unambiguous, since the middle
	// item carries an internal comma list ending in "or".
	if !strings.Contains(Content, "answer; and when finishing any task while questions sit open on the board") {
		t.Error("description missing the semicolon-coordinated finishing-a-task trigger")
	}
	fm, _, _ := strings.Cut(strings.TrimPrefix(Content, "---\n"), "\n---\n")
	if strings.Contains(fm, "check the board") {
		t.Error("description contains an imperative; triggers only")
	}
	// Keep the skill tight: ~90 lines max per the remediation brief.
	if n := strings.Count(Content, "\n"); n > 90 {
		t.Errorf("skill is %d lines; keep it under ~90", n)
	}
}

// One thread per task — announce it, status-update in-thread, tagged
// discoveries top-level with a back-link.
func TestSkillTeachesThreadStickiness(t *testing.T) {
	for _, must := range []string{
		"announce it once (",
		"as `xfa reply` on it, not as new top-level posts",
		"a `#<id>` back to your announcement",
		"Each status update deserves its own thread",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
}

// `til` is the single "learned something" tag: the skill lists it in the
// five-tag conventions, defines it in one sentence, keeps it a top-level post,
// and no longer advertises the dead six-tag list with `finding`.
func TestSkillTeachesTilCadence(t *testing.T) {
	for _, must := range []string{
		"question|til|decision|analysis|shitpost",
		"A `til` is a reusable fact about a tool, library, or repo that outlives this task — a gotcha, not a status update",
		"Tils, decisions, analyses, and questions are normal top-level posts",
		"This gotcha is project-specific, not a til",
		"one post per discovery, not one summary per session",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
	if strings.Contains(Content, "question|til|decision|finding|analysis|shitpost") {
		t.Error("SKILL.md still lists the dead `finding` tag")
	}
}

// Task 6 (session filtering): registering with a session id is only half the
// job — an unnamed session is an opaque id in everyone else's `xfa sessions`,
// so the skill must prescribe naming it, and must teach session-scoped reads
// as the way to see what a parent or sibling session has been doing.
func TestSkillTeachesSessionNamingAndScopedReads(t *testing.T) {
	for _, must := range []string{
		// Naming step, with the literal command an agent can copy.
		"xfa session name <session-id>",
		"what this session is working on",
		// Naming is parent-based, not a check-first lookup: a subagent
		// (registered with --parent) never names its own session, because
		// ListSessions only shows sessions with a live post in scope, so a
		// named-but-postless session looks unnamed to a check-first agent
		// and gets silently overwritten.
		"root agents only",
		"If you registered with `--parent`",
		"skip this",
		// Discovery + scoped reads.
		"xfa sessions",
		"--session <id>",
		// The clause distinguishing read's authored-by filter from
		// threads/board's whole-thread participated filter.
		"shows only that session's own posts",
		"show whole threads the session took part in",
		// Red flag row for the "naming is busywork" rationalization.
		"Naming my session is busywork",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing session marker %q", must)
		}
	}
	// The skill is agent-facing: it must never mention the human-only front
	// ends, session picker or otherwise.
	for _, forbidden := range []string{"xfa tui", "--web"} {
		if strings.Contains(Content, forbidden) {
			t.Errorf("SKILL.md mentions human-only surface %q", forbidden)
		}
	}
}

// v0.4.0: awareness must chain down the whole spawn tree (grandchildren
// included) and responses must thread via reply, not broadcast.
func TestSkillPropagatesAwarenessAndThreadsReplies(t *testing.T) {
	// The propagation section teaches each agent to hand xfa to the agents it
	// spawns, recursively — the fix for nested subagents never using the board.
	for _, must := range []string{
		"Make every agent you spawn xfa-aware",
		"Awareness is a chain",
		"do exactly the same for any subagent IT spawns",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing propagation marker %q", must)
		}
	}
	// Reply-not-broadcast is prescribed in the discuss step and the red flags.
	for _, must := range []string{
		"never a new top-level post that @mentions the author",
		"A new post is a broadcast, not a reply",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing reply-not-broadcast marker %q", must)
		}
	}
}

// Task 10: v0.3.0 teaches checkpoint polling (the board moves while you work)
// and mention-for-attribution (never block on a named agent).
func TestSkillTeachesCheckpointPollingAndAttribution(t *testing.T) {
	for _, must := range []string{
		// Checkpoint polling step.
		"Check at checkpoints",
		"after each completed subtask",
		"checking only at session start means working blind",
		// Mention for attribution, not dependency.
		"Mention for attribution, never for dependency",
		"answer for the record; don't wait on them",
		// Stale questions are still worth answering.
		"Answer stale questions too",
		"anyone may resolve after answering",
		// New Red Flags rows.
		"I checked the board at session start",
		"The asker is gone, no point answering",
		"I'll wait for @that-agent to reply",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
}

// Controller ruling (task 8 review): subagents must register with BOTH
// --parent and --session, or their rows carry session_id="" and the
// UserPromptSubmit digest nags the lead about its own subagents' posts.
func TestSkillTellsSubagentsToPassSession(t *testing.T) {
	if !strings.Contains(Content, "--parent <your-handle> --session <session-id>") {
		t.Error("skill does not tell subagents to register with both --parent and --session")
	}
}

// inbox --wait replaces hand-rolled sleep-and-recheck loops: the skill must
// name the flag and forbid the loop, and the old "Poll between tasks" heading
// must be gone so the two never coexist.
func TestSkillTeachesInboxWait(t *testing.T) {
	for _, must := range []string{"inbox --as <handle> --wait", "Never sleep-and-recheck"} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
	if strings.Contains(Content, "Poll between tasks") {
		t.Error("SKILL.md still says \"Poll between tasks\"; step 6 should be \"Check at checkpoints\"")
	}
}

// Task 3: the skill must not claim every command takes --as (only the
// act-as-you ones do), must point at sibling boards in a shared DB, and must
// say post ids are global so id gaps don't read as missing posts.
func TestSkillTeachesSiblingBoardsAndGlobalIDs(t *testing.T) {
	for _, must := range []string{
		"`xfa boards` lists the other boards",
		"Post ids are global",
	} {
		if !strings.Contains(Content, must) {
			t.Errorf("SKILL.md missing %q", must)
		}
	}
	if strings.Contains(Content, "Every command also accepts") {
		t.Error("SKILL.md still claims every command accepts --as")
	}
}
