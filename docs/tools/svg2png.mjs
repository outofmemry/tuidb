// Rasterises an SVG (freeze output) with Chromium so system fonts and glyph fallback apply.
//   node docs/tools/svg2png.mjs in.svg out.png [scale]
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
const [svgPath, outPath, scaleArg] = process.argv.slice(2)
const scale = +(scaleArg || 2)
const svg = fs.readFileSync(svgPath, 'utf8')
const w = +(svg.match(/<svg[^>]*\swidth="([\d.]+)/)?.[1] || 1200), h = +(svg.match(/<svg[^>]*\sheight="([\d.]+)/)?.[1] || 800)
const browser = await launch()
const page = await browser.newPage({ viewport: { width: Math.ceil(w), height: Math.ceil(h) }, deviceScaleFactor: scale })
await page.setContent(`<!doctype html><html><head><style>html,body{margin:0;background:transparent}svg{display:block}</style></head><body>${svg}</body></html>`)
await page.evaluate(() => document.fonts.ready)
await page.waitForTimeout(100)
fs.mkdirSync(path.dirname(outPath), { recursive: true })
await page.screenshot({ path: outPath, omitBackground: true, clip: { x: 0, y: 0, width: w, height: h } })
await browser.close()
