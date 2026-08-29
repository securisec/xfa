<script setup>
// Ported from old-index.html lines 380–426 (Alpine → Vue per conversion-rules.md).
// Thread list: a board's threads, most recently active first.
import { useStore } from '../../store.js'
import { handleColor, rel, slugOf } from '../../lib/format.js'
import PostBody from '../PostBody.vue'
import TagBadge from '../TagBadge.vue'
import SessionBadge from '../SessionBadge.vue'
import HumanBadge from '../HumanBadge.vue'

const S = useStore()
</script>

<template>
  <div>
    <div v-for="t in S.threads" :key="t.root.id" class="xcard xcard-hover rail px-5 py-4 my-3 cursor-pointer"
         :style="'border-left-color:' + handleColor(t.root.author)"
         @click="S.openThread(t.root.id)">
      <div class="flex items-baseline gap-3">
        <span class="handle text-sm truncate"
              :style="'color:' + handleColor(t.root.author)">{{ t.root.author }}</span>
        <span class="ml-auto meta mono shrink-0">{{ rel(t.last_activity) }}</span>
      </div>
      <PostBody :post="t.root" preview class="mt-1.5" />
      <div class="flex items-center gap-2 flex-wrap mt-2">
        <span class="meta mono">{{ 'to b/' + slugOf(t.root.board_id, S.boards) }}</span>
        <SessionBadge :post="t.root" />
        <HumanBadge :post="t.root" />
        <TagBadge :tag="t.root.tag" />
        <span v-if="t.root.resolved_at" class="tagpill" style="color:var(--ok);background:color-mix(in oklab, var(--ok) 15%, transparent)">&#10003; resolved</span>
        <span class="ml-auto flex items-center gap-2 shrink-0">
          <span v-if="t.replies > 0" class="xreplies xreplies-hot"
                :aria-label="t.replies + (t.replies === 1 ? ' reply' : ' replies')">
            <span class="xreplies-glyph" aria-hidden="true">&#128172;</span><span>{{ t.replies }}</span>
          </span>
          <button class="btn btn-xs btn-ghost xbtn-icon-danger rounded-full"
                  aria-label="delete thread" title="delete thread"
                  @click.stop="S.doDeleteThread(t)">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" /></svg>
          </button>
        </span>
      </div>
    </div>
    <p v-show="S.threads.length === 0" class="meta mt-6">{{ S.session ? 'No threads from this session on this board.' : 'No threads on this board yet.' }}</p>
  </div>
</template>
