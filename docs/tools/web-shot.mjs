// Drives the web UI with Playwright and saves PNG screenshots.
//
//   node docs/tools/web-shot.mjs --out docs/static/img/web/start.png
//   node docs/tools/web-shot.mjs --out x.png --steps '[{"click":"text=Shop (Postgres)"},{"wait":1500}]'
//   node docs/tools/web-shot.mjs --out x.png --steps-file steps.json
//   node docs/tools/web-shot.mjs --out x.png --script my-steps.mjs      // export default async (page, ctx) => {...}
//
// Options: --width 1440 --height 900 (viewport), --light (light colour scheme), --mobile (390x844 phone),
//          --full (full-page capture), --clip "x,y,w,h" (crop), --base http://127.0.0.1:7797 --token devtoken,
//          --text (also print the page's visible text, to check what was captured)
// Step objects (one key each, run in order):
//   {"goto":"/path"}                       navigate (the token fragment is appended)
//   {"click":"selector"}  {"dblclick":..} {"rightclick":..} {"hover":..}   Playwright selectors: css, text=, role=button[name="Run"]
//   {"fill":["selector","text"]}           set an input's value
//   {"type":"text"}                        type into the focused element
//   {"press":"Control+d"}                  key chord (Playwright names: Enter, Escape, ArrowDown, Alt+f, Shift+Tab ...)
//   {"keys":["Control+d","k"]}             several presses in a row (a prefix sequence)
//   {"wait":800}                           milliseconds
//   {"waitFor":"selector"}                 wait until visible
//   {"scroll":["selector", 400]}           scroll an element down by N px
//   {"eval":"document.title"}              run JS in the page
//   {"shot":"other.png"}                   extra screenshot mid-way
//   {"viewport":[1200,800]}                resize
import { chromium } from 'playwright'
// Browser: Playwright's own Chromium when installed, else an installed Chromium-based browser (Brave, Chrome, Edge).
async function launch() {
  const candidates = [
    () => chromium.launch(),
    () => chromium.launch({ channel: 'chromium' }),
    () => chromium.launch({ channel: 'chrome' }),
    () => chromium.launch({ executablePath: '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser' }),
    () => chromium.launch({ channel: 'msedge' }),
  ]
  let err
  for (const c of candidates) { try { return await c() } catch (e) { err = e } }
  throw err
}
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const args = process.argv.slice(2)
const opt = (name, dflt) => { const i = args.indexOf('--' + name); return i >= 0 ? args[i + 1] : dflt }
const flag = (name) => args.includes('--' + name)
const out = opt('out'); if (!out) { console.error('need --out file.png'); process.exit(2) }
// Every invocation is appended to docs/tools/shots.log so a screenshot can be regenerated later (docs/tools/reshoot.sh).
fs.appendFileSync(new URL('./shots.log', import.meta.url), 'web\t' + args.map((a) => /^[\w./=:-]+$/.test(a) ? a : "'" + a.replace(/'/g, "'\\''") + "'").join(' ') + '\n')
const base = opt('base', process.env.WEBTUIDB_WEB_URL || 'http://127.0.0.1:7797')
const token = opt('token', process.env.WEBTUIDB_WEB_TOKEN || 'devtoken')
const mobile = flag('mobile')
const width = +opt('width', mobile ? 390 : 1440), height = +opt('height', mobile ? 844 : 900)
const withToken = (p) => base + (p.startsWith('/') ? p : '/' + p) + (p.includes('#') ? '&' : '#') + 'token=' + token

let steps = []
if (opt('steps')) steps = JSON.parse(opt('steps'))
if (opt('steps-file')) steps = JSON.parse(fs.readFileSync(opt('steps-file'), 'utf8'))

const browser = await launch()
const ctx = await browser.newContext({
  viewport: { width, height }, deviceScaleFactor: 2, colorScheme: flag('light') ? 'light' : 'dark',
  isMobile: mobile, hasTouch: mobile, reducedMotion: 'reduce',
})
const page = await ctx.newPage()
page.setDefaultTimeout(8000)
const errors = []
// Sessions this run opened: closed again at the end, because the server keeps at most 32 open and a shot never disconnects.
const opened = []
page.on('response', async (r) => {
  try { if (r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/sessions' && r.ok()) opened.push((await r.json()).id) } catch {}
})
page.on('pageerror', (e) => errors.push(String(e)))
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()) })

const shot = async (file) => {
  fs.mkdirSync(path.dirname(file), { recursive: true })
  const clip = opt('clip') ? (([x, y, w, h]) => ({ x, y, width: w, height: h }))(opt('clip').split(',').map(Number)) : undefined
  await page.screenshot({ path: file, fullPage: flag('full'), clip, animations: 'disabled' })
  console.error('wrote ' + file)
}
const helpers = { shot, withToken, base, token }

const run = async (s) => {
  const [k, v] = Object.entries(s)[0]
  switch (k) {
    case 'goto': await page.goto(withToken(v), { waitUntil: 'networkidle' }); break
    case 'click': await page.locator(v).first().click(); break
    case 'dblclick': await page.locator(v).first().dblclick(); break
    case 'rightclick': await page.locator(v).first().click({ button: 'right' }); break
    case 'hover': await page.locator(v).first().hover(); break
    case 'fill': await page.locator(v[0]).first().fill(v[1]); break
    case 'type': await page.keyboard.type(v, { delay: 20 }); break
    case 'press': await page.keyboard.press(v); break
    case 'keys': for (const key of v) { await page.keyboard.press(key); await page.waitForTimeout(150) } break
    case 'wait': await page.waitForTimeout(v); break
    case 'waitFor': await page.locator(v).first().waitFor({ state: 'visible' }); break
    case 'scroll': await page.locator(v[0]).first().evaluate((el, dy) => el.scrollBy(0, dy), v[1]); break
    case 'eval': console.log(JSON.stringify(await page.evaluate(v))); break
    case 'shot': await shot(v); break
    case 'viewport': await page.setViewportSize({ width: v[0], height: v[1] }); break
    default: throw new Error('unknown step ' + k)
  }
}

try {
  await page.goto(withToken('/'), { waitUntil: 'networkidle' })
  if (opt('script')) {
    const mod = await import(pathToFileURL(path.resolve(opt('script'))).href)
    await mod.default(page, helpers)
  }
  for (const s of steps) await run(s)
  await page.waitForTimeout(+opt('settle', 400))
  await shot(out)
  if (flag('text')) console.log(await page.evaluate(() => document.body.innerText))
  if (errors.length) console.error('page errors:\n' + errors.join('\n'))
} catch (e) {
  console.error('failed: ' + (e?.message || e))
  try { await shot(out.replace(/\.png$/, '.failed.png')) } catch {}
  process.exitCode = 1
} finally {
  for (const id of opened) { try { const st = await page.evaluate(([id, tk]) => fetch('/api/sessions/' + id, { method: 'DELETE', headers: { Authorization: 'Bearer ' + tk } }).then((r) => r.status), [id, token]); if (process.env.SHOT_DEBUG) console.error('closed session', id, st) } catch (e) { if (process.env.SHOT_DEBUG) console.error('close failed', String(e)) } }
  await browser.close()
}
