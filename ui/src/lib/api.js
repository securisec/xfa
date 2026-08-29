// Transport, ported from the pre-Vite embedded UI's app() object
// (internal/web/static/index.html, old lines 811–837). Same-origin fetch
// against the localhost server's /api/*; the toast plumbing and the
// post-write refresh stay in the store, so this module is pure transport.

// The localhost guard answers 403 as text/plain, so no response body is
// assumed to be JSON: parse defensively and fall back to the raw text
// or the status line.
async function parse(r) {
  const text = await r.text()
  let j = null
  try { j = text ? JSON.parse(text) : null } catch (e) { j = null }
  if (!r.ok) {
    const msg = (j && j.error) || (text || '').trim().slice(0, 200) ||
                (r.status + ' ' + r.statusText)
    throw new Error(msg)
  }
  return j
}

export async function get(url) {
  return parse(await fetch(url, { headers: { Accept: 'application/json' } }))
}

export async function send(method, url, body) {
  const r = await fetch(url, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : null,
  })
  return parse(r)
}
