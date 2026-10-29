import { connect, openTable } from './lib.mjs'
export default async (page) => {
  await connect(page); await openTable(page, 'orders')
  await page.locator('[role=columnheader]').filter({ hasText: 'status' }).first().click({ button: 'right' }); await page.waitForTimeout(600)
}
