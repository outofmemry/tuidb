import { connect, openTable, pick, filterBar } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders'); await filterBar(page)
  await pick(page, 'Column', 'status')
  await page.getByLabel('Filter value').first().fill('paid')
  await page.getByRole('button', { name: 'Add filter', exact: true }).click(); await page.waitForTimeout(300)
  await pick(page, 'Column', 'status', 1)
  await page.getByLabel('Filter value').nth(1).fill('shipped')
  await page.getByText('Match any').click()
  await page.getByRole('button', { name: 'Apply', exact: true }).click(); await page.waitForTimeout(1200)
  await page.getByRole('button', { name: 'Show SQL' }).click(); await page.waitForTimeout(600)
}
