<script setup>
// Ported from old-index.html lines 322–361 (Alpine → Vue per conversion-rules.md).
import { useStore } from '../store.js'

const S = useStore()
</script>

<template>
  <div class="max-w-4xl mx-auto px-6 pt-6 flex flex-wrap items-center gap-3">
    <!-- View switching lives in the TopBar icon nav; this row keeps the
         board label, session filter and search. -->
    <span class="meta mono">{{ S.board ? 'b/' + S.board : 'all boards' }}</span>
    <!-- Session filter. Shown only on a board's thread list, because that is
         the only view the ?session= filter reaches — a picker that changed
         nothing would be worse than no picker. "all sessions" is the default
         and renders exactly the unfiltered view. -->
    <div class="md:hidden flex items-center gap-2" v-show="S.view === 'threads' && S.board !== ''">
      <select class="select select-sm xpill rounded-full mono w-56"
              aria-label="filter by session"
              v-model="S.session" @change="S.pickSession(S.session)">
        <option value="">all sessions</option>
        <option v-for="s in S.sessions" :key="s.session_id" :value="s.session_id">{{ s.display_name }}</option>
      </select>
      <button class="btn btn-xs rounded-full xbtn-quiet mono" v-show="S.session !== ''"
              title="name this session" @click="S.openRename()">rename</button>
      <button class="btn btn-xs btn-ghost xbtn-icon-danger rounded-full" v-show="S.session !== ''"
              aria-label="delete session" title="delete session"
              @click="S.deleteSelectedSession()">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" /></svg>
      </button>
    </div>
    <div class="ml-auto relative">
      <span class="absolute left-3 top-1/2 -translate-y-1/2 muted text-sm pointer-events-none">&#128269;</span>
      <input type="search" placeholder="search"
             class="xfield rounded-full pl-9 pr-4 py-1.5 text-sm w-56"
             v-model="S.q" @keydown.enter.prevent="S.runSearch()">
    </div>
  </div>
</template>
