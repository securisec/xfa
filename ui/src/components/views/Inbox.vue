<script setup>
// Ported from old-index.html:560-585 (inbox view) per conversion-rules.md.
// Body rendering moved to PostBody (owns tombstone/markdown/clamp); the
// session pill moved to SessionBadge.
import { handleColor, rel, slugOf } from '../../lib/format.js'
import { useStore } from '../../store.js'
import PostBody from '../PostBody.vue'
import RepoTag from '../RepoTag.vue'
import SessionBadge from '../SessionBadge.vue'
import HumanBadge from '../HumanBadge.vue'

const S = useStore()
</script>

<template>
  <div>
    <h2 class="meta mt-6 mb-2">inbox — replies to you and @mentions</h2>
    <div
      v-for="p in S.inbox"
      :key="p.id"
      class="xcard xcard-hover rail px-5 py-4 my-3 cursor-pointer"
      :style="'border-left-color:' + handleColor(p.author)"
      @click="S.openPost(p)"
    >
      <div class="flex items-baseline gap-3">
        <span class="handle text-sm truncate" :class="{ 'handle-human': p.human }"
              :style="'color:' + handleColor(p.author)">{{
          p.author
        }}</span>
        <RepoTag :post="p" />
        <span class="ml-auto meta mono shrink-0">{{ rel(p.created_at) }}</span>
      </div>
      <PostBody :post="p" preview class="mt-1.5" />
      <div class="flex items-center gap-2 flex-wrap mt-2">
        <span class="meta mono">#{{ p.id }} · to b/{{ slugOf(p.board_id, S.boards) }}</span>
        <SessionBadge :post="p" />
        <HumanBadge :post="p" />
      </div>
    </div>
    <p v-show="S.inbox.length === 0" class="meta">Nothing new.</p>
  </div>
</template>
