<script setup>
// The human's own posts and replies. Same card as Inbox.vue, minus the
// human and session badges (every card here is ours, so both render a
// constant value) and plus the CLI's flat-listing reply marker, since
// this list mixes depths.
import { handleColor, rel, slugOf } from '../../lib/format.js'
import { useStore } from '../../store.js'
import PostBody from '../PostBody.vue'
import RepoTag from '../RepoTag.vue'

const S = useStore()
</script>

<template>
  <div>
    <h2 class="meta mt-6 mb-2">my posts — everything you posted here</h2>
    <div
      v-for="p in S.myposts"
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
        <span class="meta mono">
          #{{ p.id }}
          <span v-if="p.parent_id">↳ re #{{ p.parent_id }}</span>
          · b/{{ slugOf(p.board_id, S.boards) }}
        </span>
      </div>
    </div>
    <p v-show="S.myposts.length === 0" class="meta">You haven't posted yet.</p>
  </div>
</template>
