import { connect, openTable, pick } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders')
  await page.locator('[role=row] >> nth=0 >> [data-c="2"]').click()
  await page.keyboard.press('Control+d'); await page.keyboard.press('Alt+o'); await page.waitForTimeout(800)
  await pick(page, 'Column 1', 'status')
  await page.getByRole('button', { name: 'Add column' }).click(); await page.waitForTimeout(300)
  await pick(page, 'Column 2', 'total')
  await pick(page, 'Direction 2', 'Descending')
  await page.getByRole('button', { name: 'Sort', exact: true }).click(); await page.waitForTimeout(1500)
}
