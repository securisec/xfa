<script setup>
import { computed } from 'vue'
import { sessionLabel } from '../lib/format.js'
import { useStore } from '../store.js'

const props = defineProps({
  post: { type: Object, required: true },
})

const S = useStore()

// Empty for a post with no session, which is also the badge's v-if: a post
// an agent wrote with no session id has no session to name.
const label = computed(() => sessionLabel(props.post))
</script>

<template>
  <span
    v-if="label"
    class="sesspill"
    :class="S.sessionOn(post)"
    :title="'session ' + post.session_id"
    >{{ label }}</span
  >
</template>
