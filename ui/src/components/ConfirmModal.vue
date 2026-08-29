<script setup>
// The app's one confirmation dialog: S.confirm({title, body, hint, action,
// danger}) opens it and resolves with the answer (see store.js). Same
// daisyUI modal shell as RenameModal; escape/backdrop/cancel all answer no.
import { useStore } from '../store.js'

const S = useStore()
</script>

<template>
  <div class="modal" :class="{'modal-open': S.dialog.open}"
       @keydown.escape.window="S.dialog.open && S.answerDialog(false)">
    <div class="modal-box rounded-lg max-w-md" style="background: var(--raised); border: 1px solid var(--border)">
      <h3 class="mono accent text-base mb-1">{{ S.dialog.title }}</h3>
      <p class="text-sm">{{ S.dialog.body }}</p>
      <p v-if="S.dialog.hint" class="meta mono mt-1">{{ S.dialog.hint }}</p>
      <div class="flex items-center gap-3 mt-4 justify-end">
        <button class="btn btn-sm xbtn-quiet rounded-full"
                @click="S.answerDialog(false)">cancel</button>
        <button class="btn btn-sm rounded-full" :class="S.dialog.danger ? 'xbtn-danger' : 'xbtn-accent'"
                :disabled="S.sending" @click="S.answerDialog(true)">{{ S.dialog.action }}</button>
      </div>
    </div>
    <div class="modal-backdrop" @click="S.answerDialog(false)"></div>
  </div>
</template>
