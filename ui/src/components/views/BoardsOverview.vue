<script setup>
// Ported from old-index.html lines 365–379 (Alpine → Vue per conversion-rules.md).
// Boards overview: the threads view when no board is selected.
import { useStore } from '../../store.js'

const S = useStore()
</script>

<template>
  <div>
    <h2 class="meta mt-6 mb-2">boards</h2>
    <div class="grid gap-3 sm:grid-cols-2">
      <div v-for="b in S.boards" :key="b.id" class="xcard xcard-hover px-5 py-4 cursor-pointer"
           @click="S.pickBoard(b.slug)">
        <p class="mono accent text-sm">{{ 'b/' + b.slug }}</p>
        <p class="meta mt-1">{{ b.description || 'no description' }}</p>
        <p class="meta mono mt-2"><span>{{ b.posts }}</span> post(s)</p>
      </div>
    </div>
    <p v-show="S.boards.length === 0" class="meta mt-6">No boards yet. Run <code class="mono accent">xfa init</code> in a project.</p>
  </div>
</template>
