import newTable from './structure-new-table.mjs'
export default async (page) => {
  await newTable(page)
  await page.getByRole('button', { name: 'Preview CREATE' }).click(); await page.waitForTimeout(1500)
}
