<script setup>
// Ported from old-index.html lines 276–317 (Alpine → Vue per conversion-rules.md).
// Boards list, with the active board's sessions nested underneath it.
import { useStore } from '../store.js'

const S = useStore()
</script>

<template>
  <aside class="hidden md:flex md:flex-col shrink-0 w-60 border-r sticky top-0 self-start max-h-screen overflow-y-auto py-6 px-3"
         style="border-color: var(--border)">
    <h2 class="meta px-2 mb-2">boards</h2>
    <a href="#/" class="navlink block px-2 py-1 rounded"
       :class="S.board === '' ? 'is-active' : ''"
       @click.prevent="S.pickBoard('')">all</a>
    <div v-for="b in S.boards" :key="b.id" class="mt-0.5">
      <a href="#" class="navlink block px-2 py-1 rounded truncate"
         :class="S.board === b.slug ? 'is-active' : ''"
         :title="'b/' + b.slug"
         @click.prevent="S.pickBoard(b.slug)">{{ 'b/' + b.slug }}</a>
      <!-- Sessions of the active board, nested. Loaded for whichever board is
           selected, so switching boards re-nests that board's sessions. -->
      <div v-show="S.board === b.slug" class="ml-3 mt-1 mb-2 border-l pl-2"
           style="border-color: var(--border)">
        <a href="#" class="navlink block px-2 py-0.5 rounded text-xs"
           :class="S.session === '' ? 'is-active' : ''"
           @click.prevent="S.pickSession('')">all sessions</a>
        <div v-for="s in S.sessions" :key="s.session_id" class="flex items-center gap-1">
          <a href="#" class="navlink block px-2 py-0.5 rounded text-xs truncate flex-1 min-w-0"
             :class="S.session === s.session_id ? 'is-active' : ''"
             :title="s.display_name"
             @click.prevent="S.pickSession(s.session_id)">{{ s.display_name }}</a>
          <button class="btn btn-xs xbtn-quiet rounded-full mono px-1 shrink-0"
                  v-show="S.session === s.session_id"
                  title="rename this session" @click="S.openRename()">&#9998;</button>
          <button class="btn btn-xs btn-ghost xbtn-icon-danger rounded-full shrink-0"
                  aria-label="delete session" title="delete session"
                  @click="S.doDeleteSession(s)">
            <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-4 h-4" width="16" height="16" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="m14.74 9-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 0 1-2.244 2.077H8.084a2.25 2.25 0 0 1-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 0 0-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 0 1 3.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 0 0-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 0 0-7.5 0" /></svg>
          </button>
        </div>
        <p v-show="S.sessions.length === 0" class="meta px-2 py-0.5 text-xs">no sessions</p>
      </div>
    </div>
  </aside>
</template>
