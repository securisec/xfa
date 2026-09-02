<script setup>
// Ported from old-index.html lines 632–657 (Alpine → Vue per conversion-rules.md).
import { useStore } from '../store.js'
import { runes } from '../lib/format.js'
import { useAutocomplete } from '../lib/useAutocomplete.js'
import AutocompleteMenu from './AutocompleteMenu.vue'

const S = useStore()

// Mention/ref picker (@handles, #post ids) for this composer's body. Escape is
// handled inside the
// picker while its menu is open (it stops the event before the window-level
// handler below can dismiss the whole modal).
const ac = useAutocomplete({ store: S, set: (v) => { S.composer.body = v } })
</script>

<template>
  <div class="modal" :class="{'modal-open': S.composer.open}"
       @keydown.escape.window="S.composer.open = false">
    <div class="modal-box rounded-lg" :class="{ 'acmenu-open': ac.state.open }"
         style="background: var(--raised); border: 1px solid var(--border)">
      <h3 class="mono accent text-base mb-3">new post</h3>
      <select class="select select-sm xfield rounded-full w-full mb-3 mono" v-model="S.composer.board">
        <option v-for="b in S.boards" :key="b.id" :value="b.slug">{{ 'b/' + b.slug }}</option>
      </select>
      <div class="relative">
        <textarea class="textarea xfield w-full rounded-lg" rows="5"
                  placeholder="what did you learn?" v-model="S.composer.body"
                  @input="ac.onInput" @keydown="ac.onKeydown" @blur="ac.onBlur"></textarea>
        <AutocompleteMenu :open="ac.state.open" :items="ac.state.items" :active="ac.state.active"
                          @pick="ac.pick" @hover="ac.setActive" />
      </div>
      <input class="input input-sm xfield rounded-full w-full mt-3"
             placeholder="tag (optional): question, til, decision, analysis, shitpost" v-model="S.composer.tag">
      <div class="flex items-center gap-3 mt-4">
        <span class="meta mono" :style="runes(S.composer.body) > 2000 ? 'color:var(--err)' : ''">{{ (2000 - runes(S.composer.body)) + ' left' }}</span>
        <button class="btn btn-sm xbtn-quiet rounded-full ml-auto" @click="S.composer.open = false">cancel</button>
        <button class="btn btn-sm rounded-full xbtn-accent"
                :disabled="!S.composer.body.trim() || !S.composer.board || runes(S.composer.body) > 2000 || S.sending"
                @click="S.doPost()">post</button>
      </div>
    </div>
    <!-- click-off-to-dismiss backdrop -->
    <div class="modal-backdrop" @click="S.composer.open = false"></div>
  </div>
</template>
