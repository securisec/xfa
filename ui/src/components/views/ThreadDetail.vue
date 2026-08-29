<script setup>
// Ported from old-index.html lines 427–495 (Alpine → Vue per
// conversion-rules.md). The outer `x-show="view === 'thread'"` is gone: App.vue
// owns this view's visibility, so the root here is the section's wrapper div.
//
// The body / expander / tombstone markup the old rows carried inline is now
// PostBody's job — including the double-click-to-toggle gesture, which moved
// onto the body itself, so the old card-level @dblclick is deliberately not
// ported (the helpers it called no longer exist outside that component).
import { watch } from 'vue'
import { useStore } from '../../store.js'
import { handleColor, rel, runes } from '../../lib/format.js'
import { useAutocomplete } from '../../lib/useAutocomplete.js'
import PostBody from '../PostBody.vue'
import TagBadge from '../TagBadge.vue'
import SessionBadge from '../SessionBadge.vue'
import HumanBadge from '../HumanBadge.vue'
import AutocompleteMenu from '../AutocompleteMenu.vue'

const S = useStore()

// One mention/ref picker (@handles, #post ids) per composer. The inline one is
// inside the v-for, but
// the store guarantees at most one is open at a time, so a single instance is
// enough — it binds to whichever textarea the events come from.
const acInline = useAutocomplete({ store: S, set: (v) => { S.inline.body = v } })
const acReply = useAutocomplete({ store: S, set: (v) => { S.reply.body = v } })

// The inline composer's textarea is removed whenever the store moves the
// composer — a hashchange (applyRoute → closeInline), the poll dropping a
// hard-deleted post's row, or simply opening the composer on another post.
// Removal fires no blur, so without this the menu stays open and the NEXT
// inline composer inherits it: its first Enter would be swallowed and splice a
// completion built from the previous textarea into an empty one. This view
// stays mounted throughout, so watching the id is the reliable hook —
// useAutocomplete's own onBeforeUnmount only covers hosts that unmount.
watch(() => S.inline.parentId, () => acInline.close())

// Reply depth is shown with a small, capped indent plus the quiet left rail —
// never enough to narrow the card. Deep threads stay as readable as the root.
function indentStyle(depth) {
  return 'margin-left:' + Math.min(depth, 6) * 0.85 + 'rem'
}
</script>

<template>
  <div>
    <button class="btn btn-sm xbtn-quiet rounded-full mono mt-4" @click="S.go('threads')">&larr; back</button>
    <div v-for="r in S.threadRows()" :key="r.post.id"
         class="my-3" :style="indentStyle(r.depth)"
         :class="r.depth > 0 ? 'indent pl-3' : ''">
      <div class="xcard px-5 py-4"
           :class="r.depth > 0 ? 'rail-sm' : 'rail'"
           :style="'border-left-color:' + handleColor(r.post.author)">
        <div class="flex items-baseline gap-3">
          <span class="handle text-sm truncate"
                :style="'color:' + handleColor(r.post.author)">{{ r.post.author }}</span>
          <span class="ml-auto meta mono shrink-0">{{ rel(r.post.created_at) }}</span>
        </div>
        <!-- The old row's own <p class="… mt-1.5"> is PostBody now; the spacing
             class falls through onto its root, so no wrapper element appears
             between the header and the body. -->
        <PostBody class="mt-1.5" :post="r.post" :small="r.depth > 0" />
        <div class="flex items-center gap-2 mt-2 flex-wrap">
          <span class="meta mono">{{ '#' + r.post.id }}</span>
          <SessionBadge :post="r.post" />
          <HumanBadge :post="r.post" />
          <TagBadge :tag="r.post.tag" />
          <!-- Backlinks: posts elsewhere that reference this one by #id. Only
               the thread endpoint populates links_in, so these chips exist
               here and nowhere else. -->
          <button v-for="ref in (r.post.links_in || [])" :key="'in-' + ref.post_id"
                  type="button"
                  class="tagpill mono" :title="'referenced by #' + ref.post_id + ' on b/' + ref.board_slug"
                  @click="S.openRef(ref.post_id, ref.thread_id)">← #{{ ref.post_id }}</button>
          <!-- v-if, not v-show: the badge's style is a literal attribute here,
               but the pill must not exist at all on an unresolved post. -->
          <span v-if="r.post.resolved_at" class="tagpill"
                style="color:var(--ok);background:color-mix(in oklab, var(--ok) 15%, transparent)">{{ '✓ resolved by ' + (r.post.resolved_by || '?') }}</span>
          <span class="ml-auto flex items-center gap-2">
            <button class="btn btn-xs btn-ghost rounded-full"
                    aria-label="reply to post" title="reply to post"
                    @click="S.openInline(r.post.id)">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 15 3 9m0 0 6-6M3 9h12a6 6 0 0 1 0 12h-3" /></svg>
            </button>
            <button class="btn btn-xs btn-ghost xbtn-icon-ok rounded-full"
                    aria-label="resolve" title="resolve"
                    v-show="S.canResolve(r.post)" @click="S.doResolve(r.post.id)">
                    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m4.5 12.75 6 6 9-13.5" /></svg>
                    </button>
            <button class="btn btn-xs btn-ghost xbtn-icon-danger rounded-full"
                    aria-label="delete post" title="delete post"
                    @click="S.doDeletePost(r.post)">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" /></svg>
            </button>
          </span>
        </div>
        <!-- inline reply composer: at most one open (store enforces it) -->
        <div v-if="S.inline.parentId === r.post.id" class="mt-3">
          <div class="relative">
            <textarea class="textarea xfield w-full rounded-lg" rows="2"
                      placeholder="reply…" v-model="S.inline.body"
                      @input="acInline.onInput" @keydown="acInline.onKeydown" @blur="acInline.onBlur"></textarea>
            <AutocompleteMenu :open="acInline.state.open" :items="acInline.state.items"
                              :active="acInline.state.active"
                              @pick="acInline.pick" @hover="acInline.setActive" />
          </div>
          <div class="flex items-center gap-3 mt-2">
            <span class="meta mono" :style="runes(S.inline.body) > 2000 ? 'color:var(--err)' : ''">{{ (2000 - runes(S.inline.body)) + ' left' }}</span>
            <button class="btn btn-xs btn-ghost rounded-full" @click="S.closeInline()">cancel</button>
            <button class="btn btn-sm rounded-full ml-auto xbtn-accent"
                    :disabled="!S.inline.body.trim() || runes(S.inline.body) > 2000 || S.sending"
                    @click="S.doInlineReply()">reply</button>
          </div>
        </div>
      </div>
    </div>

    <!-- reply composer -->
    <div class="xcard px-5 py-4 mt-6" v-show="S.threadRoot()">
      <div class="relative">
        <textarea class="textarea xfield w-full rounded-lg" rows="3"
                  placeholder="reply…" v-model="S.reply.body"
                  @input="acReply.onInput" @keydown="acReply.onKeydown" @blur="acReply.onBlur"></textarea>
        <AutocompleteMenu :open="acReply.state.open" :items="acReply.state.items"
                          :active="acReply.state.active"
                          @pick="acReply.pick" @hover="acReply.setActive" />
      </div>
      <div class="flex items-center gap-3 mt-2">
        <span class="meta mono" :style="runes(S.reply.body) > 2000 ? 'color:var(--err)' : ''">{{ (2000 - runes(S.reply.body)) + ' left' }}</span>
        <button class="btn btn-sm rounded-full ml-auto xbtn-accent"
                :disabled="!S.reply.body.trim() || runes(S.reply.body) > 2000 || S.sending"
                @click="S.doReply()">reply</button>
      </div>
    </div>
  </div>
</template>
