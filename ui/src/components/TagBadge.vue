<script setup>
import { tagStyle } from '../lib/format.js'

defineProps({
  tag: { type: String, default: '' },
})
</script>

<template>
  <!-- v-if, not v-show: a :style string binding rewrites the style attribute
       wholesale and would wipe v-show's display:none, leaving an empty "[]"
       pill on untagged posts. -->
  <span v-if="tag" class="tagpill" :style="tagStyle(tag)">[{{ tag }}]</span>
</template>
