<script setup>
// The author's repo hint from registration, shown right after the handle so
// two agents on one board working in different checkouts read apart.
defineProps({ post: { type: Object, required: true } })
</script>

<template>
  <span v-if="post.repo" class="repotag meta shrink-0">({{ post.repo }})</span>
</template>
