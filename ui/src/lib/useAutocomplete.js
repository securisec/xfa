// Inline mention/reference autocomplete: the DOM half.
//
// One instance per composer. The pure decisions (what is a token, what should
// be offered, what the text becomes) live in autocomplete.js; this file only
// owns the menu's open/active state and the keyboard contract, so the thing
// that is hard to test in isolation stays as small as possible.
//
// The textarea is not passed in. Every gesture that matters arrives as an
// event on it (input, keydown, blur), so the element comes from the event and
// the caller never has to plumb a template ref through — which also means the
// composer inside a v-for (ThreadDetail's inline reply) needs no special case
// when the same instance is reused for whichever row is open.

import { reactive, nextTick, onBeforeUnmount, getCurrentInstance } from 'vue'
import { tokenAt, candidates, applyCompletion } from './autocomplete.js'

// Keys that move the caret without changing the text. The menu describes a
// token at one caret position, so once the caret leaves, the menu is a lie.
// (jsdom aside, the caret has not moved yet when keydown fires, so re-reading
// the token here would still see the old one — closing is both correct and the
// only thing that can be done synchronously.)
const CARET_KEYS = ['ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown']

export function useAutocomplete(opts) {
  const state = reactive({ open: false, items: [], active: 0 })
  let el = null       // the textarea the last event came from
  let token = null    // the {start, query, kind} the current items answer

  function close() {
    state.open = false
    state.items = []
    state.active = 0
    token = null
    // Drop the element too: a menu closed because its textarea went away must
    // not keep that detached node alive, and nothing may act on it afterwards.
    el = null
  }

  // A browser fires no blur when an element is REMOVED, so blur alone cannot
  // be trusted to close the menu. Unmounting the host is one way that happens;
  // a host that survives while its textarea comes and goes (ThreadDetail's
  // inline composer, opened and closed by the store) has to close this itself,
  // which is why close is exported. Left open, the menu greets the NEXT
  // composer already open and swallows its first Enter.
  if (getCurrentInstance()) onBeforeUnmount(close)

  // Recompute from whatever the textarea currently holds. The element is the
  // source of truth for both value and caret: v-model's own input listener may
  // not have written the store field yet, and the store field never carries a
  // caret at all.
  function refresh(target) {
    if (target) el = target
    if (!el) { close(); return }
    const t = tokenAt(el.value, el.selectionStart)
    if (!t) { close(); return }
    // The sigil the token carries picks the namespace; the menu never mixes.
    const items = candidates(opts.store, t.query, t.kind)
    if (!items.length) { close(); return }
    token = t
    state.items = items
    state.active = 0
    state.open = true
  }

  async function accept(i) {
    const item = state.items[i]
    if (!item || !el || !token) { close(); return }
    // close() drops the element reference, so the caret restoration below has
    // to hold its own handle on the textarea it is putting the caret back into.
    const target = el
    const r = applyCompletion(target.value, target.selectionStart, token, item.insert)
    // Write through the store field, not el.value: the field is what v-model
    // binds and what the send path reads, and letting Vue own the element's
    // value keeps the two from drifting.
    opts.set(r.text)
    close()
    // Setting .value collapses a textarea's selection to the end, so the caret
    // has to be put back after the render that applies it.
    await nextTick()
    if (target.setSelectionRange) {
      target.focus()
      target.setSelectionRange(r.caret, r.caret)
    }
  }

  return {
    state,
    close,
    onInput(e) { refresh(e && e.target) },
    onKeydown(e) {
      if (e && e.target) el = e.target
      // An IME candidate window is already using these keys.
      if (!state.open || (e && e.isComposing)) return
      const k = e.key
      const n = state.items.length
      // An open menu with no rows should be impossible (refresh closes instead
      // of opening empty), but the modulo below would answer NaN if it ever
      // were, and a NaN active index poisons every later comparison.
      if (!n) { close(); return }
      if (k === 'ArrowDown') { e.preventDefault(); state.active = (state.active + 1) % n; return }
      if (k === 'ArrowUp') { e.preventDefault(); state.active = (state.active - 1 + n) % n; return }
      if (k === 'Enter' || k === 'Tab') {
        // Enter would otherwise insert a newline (and Tab would leave the
        // field) on the way to accepting — the menu owns both keys while open,
        // and gives them straight back when it closes.
        e.preventDefault()
        accept(state.active)
        return
      }
      if (k === 'Escape') {
        // stopPropagation matters: ComposerModal listens for Escape on the
        // window to dismiss itself, and the first Escape belongs to the menu.
        e.preventDefault()
        e.stopPropagation()
        close()
        return
      }
      if (CARET_KEYS.includes(k)) close()
    },
    onBlur() { close() },
    pick(i) { accept(i) },
    setActive(i) { if (i >= 0 && i < state.items.length) state.active = i },
  }
}
