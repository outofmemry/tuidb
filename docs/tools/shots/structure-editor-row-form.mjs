import { connect, openStructure } from './lib.mjs'
export default async (page) => {
  await connect(page); await openStructure(page)
  await page.getByLabel('Column 6 name').click()
  await page.keyboard.press('Control+d'); await page.keyboard.press('Space'); await page.waitForTimeout(800)
}
