<script setup>
defineProps({
  post: { type: Object, required: true },
})
</script>

<template>
  <span v-if="post.project" class="tagpill" style="color:var(--color-info);background:color-mix(in oklab, var(--color-info) 15%, transparent)" :title="post.project_path">{{ post.project }}</span>
</template>
