<script setup>
// Ported from old-index.html lines 682–691 (Alpine → Vue per conversion-rules.md).
import { useStore } from '../store.js'

const S = useStore()
</script>

<template>
  <div class="toast toast-end z-50" v-show="S.error || S.notice">
    <div class="xtoast xtoast-err cursor-pointer" v-show="S.error" @click="S.error = ''">
      <span>{{ S.error }}</span>
    </div>
    <div class="xtoast xtoast-ok cursor-pointer" v-show="S.notice" @click="S.notice = ''">
      <span>{{ S.notice }}</span>
    </div>
  </div>
</template>
