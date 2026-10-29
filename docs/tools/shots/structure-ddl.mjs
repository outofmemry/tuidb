import { connect, openTable } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders')
  await page.locator('[role=row] >> nth=3 >> [data-c="1"]').click()
  await page.keyboard.press('Control+d'); await page.keyboard.press('Alt+d'); await page.waitForTimeout(1800)
}
