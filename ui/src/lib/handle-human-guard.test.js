import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

// Every view that renders a post author must chip the handle when the post is
// human-authored. Stats.vue is exempt: its top-poster rows are handles, not
// posts, so they carry no `human` field to key off.
const VIEWS = join(__dirname, '..', 'components', 'views')

describe('human handle chip', () => {
  it('every post-author handle span binds handle-human', () => {
    const authorViews = readdirSync(VIEWS)
      .filter((f) => f.endsWith('.vue') && f !== 'Stats.vue')
      .filter((f) => readFileSync(join(VIEWS, f), 'utf8').includes('class="handle'))
    // Non-vacuous: if a view is renamed or its author span disappears, this
    // count moves and the test says so rather than passing on an empty list.
    expect(authorViews.sort()).toEqual([
      'Inbox.vue', 'MyPosts.vue', 'Questions.vue',
      'SearchResults.vue', 'ThreadDetail.vue', 'ThreadList.vue',
    ])
    const offenders = authorViews.filter(
      (f) => !readFileSync(join(VIEWS, f), 'utf8').includes("'handle-human':"),
    )
    expect(offenders).toEqual([])
  })
})
