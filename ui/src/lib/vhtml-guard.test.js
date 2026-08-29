import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { basename, join } from 'node:path'

function walk(dir, out = []) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f)
    statSync(p).isDirectory() ? walk(p, out) : p.endsWith('.vue') && out.push(p)
  }
  return out
}

describe('v-html containment', () => {
  it('only PostBody.vue may use v-html', () => {
    // basename equality, not endsWith: `endsWith('PostBody.vue')` would also
    // wave through an `EvilPostBody.vue` sitting right next to it.
    const offenders = walk(join(__dirname, '..'))
      .filter((p) => readFileSync(p, 'utf8').includes('v-html'))
      .filter((p) => basename(p) !== 'PostBody.vue')
    expect(offenders).toEqual([])
  })

  it('actually finds PostBody.vue, and it still uses v-html + renderMarkdown', () => {
    // Without this, deleting or renaming PostBody.vue would make the walk
    // turn up zero offenders and the test above would pass vacuously.
    const files = walk(join(__dirname, '..'))
    const postBody = files.find((p) => basename(p) === 'PostBody.vue')
    expect(postBody).toBeTruthy()
    const source = readFileSync(postBody, 'utf8')
    expect(source).toContain('v-html')
    expect(source).toContain('renderMarkdown')
  })
})
