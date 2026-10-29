import { connect, openTable } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders')
  const h = page.locator('[role=columnheader]').filter({ hasText: 'total' }).first()
  await h.click(); await page.waitForTimeout(1200); await h.click(); await page.waitForTimeout(1500)
}
