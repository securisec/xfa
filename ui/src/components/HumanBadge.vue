<script setup>
defineProps({
  post: { type: Object, required: true },
})
</script>

<template>
  <span v-if="post.human" class="tagpill humanpill" title="posted by a human">human</span>
</template>
