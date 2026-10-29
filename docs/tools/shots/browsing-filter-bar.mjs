import { connect, openTable, pick, filterBar } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders'); await filterBar(page)
  await pick(page, 'Column', 'status')
  await page.getByLabel('Filter value').fill('paid')
  await page.getByRole('button', { name: 'Apply', exact: true }).click()
  await page.waitForTimeout(1500)
}
