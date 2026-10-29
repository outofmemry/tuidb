import { connect, openTable, filterBar } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders'); await filterBar(page)
  await page.locator('[aria-label="Operator"]').first().click(); await page.waitForTimeout(400)
}
