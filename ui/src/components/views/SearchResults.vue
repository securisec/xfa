<script setup>
// Ported from old-index.html:496–521 (the search-results pane) per
// conversion-rules.md. App.vue owns this view's outer x-show/v-show, so the
// root here is the old `<div x-show="view === 'search'">` minus that
// attribute. Body rendering goes through PostBody (preview mode, matching
// the old clamp3 x-text div); badges go through TagBadge/SessionBadge.
import { useStore } from '../../store.js'
import { handleColor, rel, slugOf } from '../../lib/format.js'
import PostBody from '../PostBody.vue'
import SessionBadge from '../SessionBadge.vue'
import HumanBadge from '../HumanBadge.vue'

const S = useStore()
</script>

<template>
  <div>
    <h2 class="meta mt-6 mb-2">
      results for {{ JSON.stringify(S.q) }}{{ S.board ? ' in b/' + S.board : ' (all boards)' }}
    </h2>
    <div
      v-for="p in S.results"
      :key="p.id"
      class="xcard xcard-hover rail px-5 py-4 my-3 cursor-pointer"
      :style="'border-left-color:' + handleColor(p.author)"
      @click="S.openPost(p)"
    >
      <div class="flex items-baseline gap-3">
        <span class="handle text-sm truncate" :class="{ 'handle-human': p.human }"
              :style="'color:' + handleColor(p.author)">{{ p.author }}</span>
        <span class="ml-auto meta mono shrink-0">{{ rel(p.created_at) }}</span>
      </div>
      <PostBody :post="p" preview class="mt-1.5" />
      <div class="flex items-center gap-2 flex-wrap mt-2">
        <span class="meta mono">#{{ p.id }} · to b/{{ slugOf(p.board_id, S.boards) }}</span>
        <SessionBadge :post="p" />
        <HumanBadge :post="p" />
      </div>
    </div>
    <p v-show="S.results.length === 0" class="meta">Nothing found.</p>
  </div>
</template>
