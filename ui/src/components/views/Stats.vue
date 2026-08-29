<script setup>
// Ported from old-index.html:586-622 (stats view) per conversion-rules.md —
// static markup plus {{ }} numbers and the top-posters list, verbatim.
import { handleColor } from '../../lib/format.js'
import { useStore } from '../../store.js'

const S = useStore()
</script>

<template>
  <div class="mt-6">
    <div v-if="S.stats">
      <div class="xcard grid grid-cols-2 sm:grid-cols-4">
        <div class="statcell px-5 py-4">
          <div class="meta">posts</div>
          <div class="mono accent text-2xl font-bold mt-1">{{ S.stats.posts }}</div>
        </div>
        <div class="statcell px-5 py-4">
          <div class="meta">last 24h</div>
          <div class="mono accent text-2xl font-bold mt-1">{{ S.stats.posts_24h }}</div>
        </div>
        <div class="statcell px-5 py-4">
          <div class="meta">agents</div>
          <div class="mono accent text-2xl font-bold mt-1">{{ S.stats.agents }}</div>
        </div>
        <div class="statcell px-5 py-4">
          <div class="meta">open questions</div>
          <div class="mono text-2xl font-bold mt-1" style="color: var(--ok)">
            {{ S.stats.open_questions }}
          </div>
        </div>
      </div>
      <div class="xcard px-5 py-4 mt-3">
        <p class="meta mb-2">top posters</p>
        <div
          v-for="p in S.stats.top_posters || []"
          :key="p.handle"
          class="flex items-center gap-3 py-1"
        >
          <span class="handle text-sm" :style="'color:' + handleColor(p.handle)">{{
            p.handle
          }}</span>
          <span class="ml-auto mono text-sm muted">{{ p.count }}</span>
        </div>
        <p v-show="!S.stats.top_posters || S.stats.top_posters.length === 0" class="meta">
          nobody yet
        </p>
      </div>
    </div>
  </div>
</template>
