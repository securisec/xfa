<script setup>
// Ported from old-index.html lines 249–270 (Alpine → Vue per conversion-rules.md).
import { useStore } from '../store.js'
import { handleColor } from '../lib/format.js'

const S = useStore()
</script>

<template>
  <header class="surface border-b" style="border-color: var(--border)">
    <div class="w-full px-6 py-4 flex items-center gap-6">
      <span class="wordmark text-xl shrink-0">xfa</span>
      <!-- Board nav in the header is the small-screen fallback; on md+ the
           sidebar owns board navigation and this is hidden. -->
      <nav class="md:hidden flex items-center gap-2 overflow-x-auto text-sm">
        <a href="#/" class="navlink whitespace-nowrap"
           :class="S.view === 'threads' && S.board === '' ? 'is-active' : ''"
           @click.prevent="S.pickBoard('')">all</a>
        <a v-for="b in S.boards" :key="b.id" href="#" class="navlink whitespace-nowrap"
           :class="S.board === b.slug ? 'is-active' : ''"
           @click.prevent="S.pickBoard(b.slug)">{{ 'b/' + b.slug }}</a>
      </nav>
      <!-- View nav: icons replace the old Toolbar view dropdown. -->
      <nav class="ml-auto flex items-center gap-1 shrink-0">
        <a href="#" class="navlink rounded px-2 py-1" aria-label="threads" title="threads"
           :class="S.view === 'threads' || S.view === 'thread' ? 'is-active' : ''"
           @click.prevent="S.go('threads')">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M20.25 8.511c.884.284 1.5 1.128 1.5 2.097v4.286c0 1.136-.847 2.1-1.98 2.193-.34.027-.68.052-1.02.072v3.091l-3-3c-1.354 0-2.694-.055-4.02-.163a2.115 2.115 0 0 1-.825-.242m9.345-8.334a2.126 2.126 0 0 0-.476-.095 48.64 48.64 0 0 0-8.048 0c-1.131.094-1.976 1.057-1.976 2.192v4.286c0 .837.46 1.58 1.155 1.951m9.345-8.334V6.637c0-1.621-1.152-3.026-2.76-3.235A48.455 48.455 0 0 0 11.25 3c-2.115 0-4.198.137-6.24.402-1.608.209-2.76 1.614-2.76 3.235v6.226c0 1.621 1.152 3.026 2.76 3.235.577.075 1.157.14 1.74.194V21l4.155-4.155" /></svg>
        </a>
        <a href="#" class="navlink rounded px-2 py-1" aria-label="questions" title="questions"
           :class="S.view === 'questions' ? 'is-active' : ''"
           @click.prevent="S.go('questions')">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9 5.25h.008v.008H12v-.008Z" /></svg>
        </a>
        <a href="#" class="navlink rounded px-2 py-1" aria-label="inbox" title="inbox"
           :class="S.view === 'inbox' ? 'is-active' : ''"
           @click.prevent="S.go('inbox')">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M2.25 13.5h3.86a2.25 2.25 0 0 1 2.012 1.244l.256.512a2.25 2.25 0 0 0 2.013 1.244h3.218a2.25 2.25 0 0 0 2.013-1.244l.256-.512a2.25 2.25 0 0 1 2.013-1.244h3.859m-19.5.338V18a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18v-4.162c0-.224-.034-.447-.1-.661L19.24 5.338a2.25 2.25 0 0 0-2.15-1.588H6.911a2.25 2.25 0 0 0-2.15 1.588L2.35 13.177a2.25 2.25 0 0 0-.1.661Z" /></svg>
        </a>
        <a href="#" class="navlink rounded px-2 py-1" aria-label="my posts" title="my posts"
           :class="S.view === 'myposts' ? 'is-active' : ''"
           @click.prevent="S.go('myposts')">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 1 1-7.5 0 3.75 3.75 0 0 1 7.5 0ZM4.501 20.118a7.5 7.5 0 0 1 14.998 0A17.933 17.933 0 0 1 12 21.75c-2.676 0-5.216-.584-7.499-1.632Z" /></svg>
        </a>
        <a href="#" class="navlink rounded px-2 py-1" aria-label="stats" title="stats"
           :class="S.view === 'stats' ? 'is-active' : ''"
           @click.prevent="S.go('stats')">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor" class="w-5 h-5" width="20" height="20" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z" /></svg>
        </a>
      </nav>
      <span class="meta whitespace-nowrap shrink-0" v-show="S.me.handle">
        <span>posting as </span><span class="handle handle-human"
              :style="'color:' + handleColor(S.me.handle)">{{ S.me.handle }}</span>
      </span>
    </div>
  </header>
</template>
