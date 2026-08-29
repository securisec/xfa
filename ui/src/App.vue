<script setup>
// App shell, ported from old-index.html's <body> (lines 247–691) per
// conversion-rules.md. S.init() replaces Alpine's auto-init (x-data="app()"
// used to run createStore().init() the moment Alpine parsed the body).
import { onMounted } from 'vue'
import { useStore } from './store.js'
import TopBar from './components/TopBar.vue'
import Sidebar from './components/Sidebar.vue'
import Toolbar from './components/Toolbar.vue'
import Toasts from './components/Toasts.vue'
import BoardsOverview from './components/views/BoardsOverview.vue'
import ThreadList from './components/views/ThreadList.vue'
import ThreadDetail from './components/views/ThreadDetail.vue'
import SearchResults from './components/views/SearchResults.vue'
import Questions from './components/views/Questions.vue'
import Inbox from './components/views/Inbox.vue'
import Stats from './components/views/Stats.vue'
import ComposerModal from './components/ComposerModal.vue'
import RenameModal from './components/RenameModal.vue'
import ConfirmModal from './components/ConfirmModal.vue'

const S = useStore()

onMounted(() => { S.init() })
</script>

<template>
  <div class="min-h-screen">
    <TopBar />

    <!-- Full-width shell: a fixed sidebar for boards (with the active board's
         sessions nested under it) and the rest of the viewport for messages. -->
    <div class="flex items-start">
      <Sidebar />

      <!-- ── message column: takes the rest of the viewport ────────────── -->
      <div class="flex-1 min-w-0">
        <Toolbar />

        <main class="max-w-4xl mx-auto px-6 pb-32 pt-2">
          <BoardsOverview v-show="S.view === 'threads' && S.board === ''" />
          <ThreadList    v-show="S.view === 'threads' && S.board !== ''" />
          <ThreadDetail  v-show="S.view === 'thread'" />
          <SearchResults v-show="S.view === 'search'" />
          <Questions     v-show="S.view === 'questions'" />
          <Inbox         v-show="S.view === 'inbox'" />
          <Stats         v-show="S.view === 'stats'" />
        </main>
      </div>
    </div>

    <!-- ── new-post button ─────────────────────────────────────────────── -->
    <button class="btn btn-circle btn-lg fixed bottom-8 right-8 xbtn-accent text-2xl"
            @click="S.openComposer()" title="new post">+</button>

    <ComposerModal />
    <RenameModal />
    <ConfirmModal />

    <Toasts />
  </div>
</template>
