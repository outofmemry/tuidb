import { connect, openTable } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders')
  await page.locator('[role=row] >> nth=3 >> [data-c="2"]').click()
  await page.locator('[role=row] >> nth=3 >> [data-c="2"]').click({ button: 'right' }); await page.waitForTimeout(500)
}
