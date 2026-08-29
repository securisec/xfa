<script setup>
// The single funnel every post body flows through — thread list previews,
// thread detail, search, questions and inbox all render here, so there is
// exactly one place that decides "is this a tombstone", "is this clipped"
// and "does this go through markdown". It is also the ONLY component in the
// app allowed to use v-html (enforced by lib/vhtml-guard.test.js): every
// string it hands to v-html has come back through renderMarkdown, which
// sanitizes with DOMPurify.
import { computed } from 'vue'
import { renderMarkdown } from '../lib/markdown.js'
import { BODY_CLIP, runes } from '../lib/format.js'
import { useStore } from '../store.js'

const props = defineProps({
  post: { type: Object, required: true },
  // List views show a three-line CSS preview of the whole body and get no
  // toggle — the clip/expander pair only exists in the detail views.
  preview: { type: Boolean, default: false },
  // Replies render one notch smaller than the root post.
  small: { type: Boolean, default: false },
})

const S = useStore()

// A tombstoned post is rendered as the literal string, never as markdown:
// the server already masks the body to "[deleted]", but routing that through
// renderMarkdown would mean a body could, in principle, decide how a deleted
// post looks. `deleted` is the API's flag (internal/web/json.go).
const tombstoned = computed(() => !!(props.post && props.post.deleted))

const rootClass = computed(() => [props.small ? 'post-body-sm' : 'post-body', 'break-words'])

// ── long-body clipping (detail views only) ───────────────────────────────
// The cut is taken on the rune array, never with String.slice: slicing by
// UTF-16 index at BODY_CLIP could land between the two halves of a surrogate
// pair and emit half an emoji. The clip is applied to the markdown SOURCE
// and the clipped source is then rendered, so a cut landing mid-construct
// (inside a code fence, say) simply renders as whatever markdown that prefix
// is — DOMPurify still guarantees the result is well-formed HTML.
function bodyOverflow(p) {
  const n = runes(p && p.body)
  return n > BODY_CLIP ? n - BODY_CLIP : 0
}

// A tombstone has no body to fold, and a preview is clamped by CSS instead,
// so neither ever grows an expander.
const overflow = computed(() =>
  props.preview || tombstoned.value ? 0 : bodyOverflow(props.post),
)

function isExpanded(id) {
  return !!S.expanded[id]
}
function toggleExpand(id) {
  S.expanded[id] = !S.expanded[id]
}
const expanded = computed(() => isExpanded(props.post && props.post.id))

const source = computed(() => {
  const body = (props.post && props.post.body) || ''
  if (props.preview || expanded.value) return body
  const chars = [...body]
  return chars.length > BODY_CLIP ? chars.slice(0, BODY_CLIP).join('') : body
})

const html = computed(() => (tombstoned.value ? '' : renderMarkdown(source.value)))

// A chevron, not a word: down = there is more below, up = fold it back.
// The collapsed state carries the count so the human knows how much more.
const expandLabel = computed(() =>
  expanded.value ? '▴' : '▾ +' + bodyOverflow(props.post).toLocaleString() + ' chars',
)

// Clicks on #id anchors navigate in-app. A recorded link (the post's own
// links_out, populated only on the thread endpoint) carries the target's
// thread root and is passed straight through; everywhere else — previews, and
// the many refs that predate post_links and so have no row — the bare id goes
// to the store, which resolves it. The event is swallowed either way: the
// anchor's href is a literal '#', which the browser would otherwise write
// over the hash route.
function onBodyClick(e) {
  const a = e.target.closest && e.target.closest('a[data-postref]')
  if (!a) return
  e.preventDefault()
  e.stopPropagation()
  const id = parseInt(a.dataset.postref, 10)
  if (!Number.isFinite(id)) return
  const ref = (props.post.links_out || []).find((l) => l.post_id === id)
  S.openRef(id, ref ? ref.thread_id : null)
}

// Double-clicking the body toggles its clip too — the chevron is a small
// target for a long post. Guard on the target rather than using `.self`, so
// the chevron and any control that ends up inside a body keep their own
// click behaviour. A post that does not overflow is a no-op — nothing to
// fold. The dblclick's default word/paragraph selection is dropped
// afterwards so a toggle does not leave the post highlighted; ordinary
// single-click selection is untouched.
//
// Propagation stops here so that a card wrapping this component cannot
// double-toggle by handling the same dblclick: every case where this handler
// returns early is a case the old card-level handler returned early on too.
function dblToggleExpand(e) {
  if (e && e.stopPropagation) e.stopPropagation()
  const t = e && e.target
  if (t && t.closest && t.closest('button,a,textarea,input,select')) return
  if (overflow.value <= 0) return
  toggleExpand(props.post.id)
  const sel = typeof window !== 'undefined' && window.getSelection ? window.getSelection() : null
  if (sel && sel.removeAllRanges) sel.removeAllRanges()
}
</script>

<template>
  <!-- Tombstone: literal text, no markdown, no expander, in either mode. -->
  <div v-if="tombstoned" :class="[rootClass, preview ? 'clamp3' : '']">
    <span class="tomb">[deleted]</span>
  </div>

  <!-- Preview: the whole body, clamped to three lines by CSS. v-html sits on
       the .clamp3 element itself — an intermediate block wrapper would break
       the -webkit-box line clamp. -->
  <div v-else-if="preview" :class="[rootClass, 'clamp3']" v-html="html" @click="onBodyClick"></div>

  <!-- Detail: clipped source rendered as markdown, plus a real button so the
       toggle is keyboard-reachable. v-if, not v-show: a post that does not
       overflow must render exactly as it did before this existed — no empty
       button. -->
  <div v-else :class="rootClass" @dblclick="dblToggleExpand">
    <div class="body-md" v-html="html" @click="onBodyClick"></div>
    <button
      v-if="overflow > 0"
      type="button"
      class="expander"
      title="or double-click the post"
      @click.stop="toggleExpand(post.id)"
    >
      {{ expandLabel }}
    </button>
  </div>
</template>
