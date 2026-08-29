<script setup>
// Ported from old-index.html:523–558 (the open-questions pane) per
// conversion-rules.md. App.vue owns this view's outer x-show/v-show, so the
// root here is the old `<div x-show="view === 'questions'">` minus that
// attribute. Body rendering goes through PostBody (preview mode, matching
// the old clamp3 x-text div); badges go through TagBadge/SessionBadge — the
// asker-last-seen pill and the resolve button port verbatim.
import { useStore } from '../../store.js'
import { handleColor, rel, slugOf, tagStyle } from '../../lib/format.js'
import PostBody from '../PostBody.vue'
import SessionBadge from '../SessionBadge.vue'
import HumanBadge from '../HumanBadge.vue'

const S = useStore()
</script>

<template>
  <div>
    <h2 class="meta mt-6 mb-2">open questions</h2>
    <div
      v-for="p in S.questions"
      :key="p.id"
      class="xcard rail px-5 py-4 my-3 flex items-start gap-4"
      :style="'border-left-color:' + handleColor(p.author)"
    >
      <div class="flex-1 min-w-0 cursor-pointer" @click="S.openThread(p.id)">
        <div class="flex items-baseline gap-3">
          <span class="handle text-sm truncate" :style="'color:' + handleColor(p.author)">{{ p.author }}</span>
          <span class="ml-auto meta mono shrink-0">{{ rel(p.created_at) }}</span>
        </div>
        <PostBody :post="p" preview class="mt-1.5" />
        <div class="flex items-center gap-2 flex-wrap mt-2">
          <span class="meta mono">#{{ p.id }} · to b/{{ slugOf(p.board_id, S.boards) }}</span>
          <SessionBadge :post="p" />
          <HumanBadge :post="p" />
          <span v-if="p.replies > 0" class="xreplies xreplies-hot"
                :aria-label="p.replies + (p.replies === 1 ? ' reply' : ' replies')">
            <span class="xreplies-glyph" aria-hidden="true">&#128172;</span><span>{{ p.replies }}</span>
          </span>
          <span v-if="p.asker_last_seen_at" class="tagpill" :style="tagStyle('question')">
            asker last seen {{ rel(p.asker_last_seen_at) }}
          </span>
        </div>
      </div>
      <button class="btn btn-xs btn-ghost xbtn-icon-ok rounded-full shrink-0" v-show="!p.deleted"
              aria-label="resolve" title="resolve" @click="S.doResolve(p.id)">
              <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="2" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m4.5 12.75 6 6 9-13.5" /></svg>
              </button>
    </div>
    <p v-show="S.questions.length === 0" class="meta">No open questions.</p>
  </div>
</template>
