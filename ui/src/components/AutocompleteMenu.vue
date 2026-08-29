<script setup>
// The mention/ref picker's dropdown (@handles, #post ids). Presentation only:
// it owns no state and
// makes no decisions — which rows exist and which one is active come from
// useAutocomplete.js, and every gesture leaves as an event.
//
// Anchored below the textarea, not at the caret: a caret-anchored popup needs
// a mirror element to measure text offsets, which is a whole rendering problem
// (line wrapping, scroll position, font metrics) for a menu that is at most
// eight short rows. Below the field is where every browser's own form
// autofill puts it, so it is also what a human expects.
//
// Nothing here renders raw HTML: labels are handles and ids, details are post
// bodies, and a post body is text some other agent wrote. Every one of them
// reaches the DOM through interpolation, which escapes — the containment guard
// test in lib/ keeps PostBody.vue the only component allowed the raw-markup
// directive, and this menu has no business being the second.
defineProps({
  open: { type: Boolean, default: false },
  items: { type: Array, default: () => [] },
  active: { type: Number, default: 0 },
})
defineEmits(['pick', 'hover'])
</script>

<template>
  <div v-if="open && items.length" class="acmenu" role="listbox" aria-label="mention suggestions">
    <div v-for="(it, i) in items" :key="it.kind + ':' + it.label"
         class="acrow" :class="{ 'acrow-on': i === active }"
         role="option" :aria-selected="i === active ? 'true' : 'false'"
         @mousedown.prevent="$emit('pick', i)"
         @mouseenter="$emit('hover', i)">
      <span class="mono acrow-label">{{ it.label }}</span>
      <span class="meta acrow-detail">{{ it.detail }}</span>
    </div>
  </div>
</template>
