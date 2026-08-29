<script setup>
// Ported from old-index.html lines 659–680 (Alpine → Vue per conversion-rules.md).
import { useStore } from '../store.js'
import { runes, MAX_SESSION_NAME } from '../lib/format.js'

const S = useStore()
</script>

<template>
  <div class="modal" :class="{'modal-open': S.rename.open}"
       @keydown.escape.window="S.rename.open = false">
    <div class="modal-box rounded-lg" style="background: var(--raised); border: 1px solid var(--border)">
      <h3 class="mono accent text-base mb-1">name this session</h3>
      <p class="meta mono mb-3 break-all">{{ S.rename.session }}</p>
      <input class="input input-sm xfield rounded-full w-full"
             placeholder="what is this session doing?"
             v-model="S.rename.name" @keydown.enter.prevent="S.doRename()">
      <div class="flex items-center gap-3 mt-4">
        <span class="meta mono" :style="runes(S.rename.name) > MAX_SESSION_NAME ? 'color:var(--err)' : ''">{{ (MAX_SESSION_NAME - runes(S.rename.name)) + ' left' }}</span>
        <button class="btn btn-sm xbtn-quiet rounded-full ml-auto"
                @click="S.rename.open = false">cancel</button>
        <button class="btn btn-sm rounded-full xbtn-accent"
                :disabled="!S.rename.name.trim() || runes(S.rename.name) > MAX_SESSION_NAME || S.sending"
                @click="S.doRename()">save</button>
      </div>
    </div>
    <!-- click-off-to-dismiss backdrop -->
    <div class="modal-backdrop" @click="S.rename.open = false"></div>
  </div>
</template>
