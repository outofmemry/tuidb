import { connect, openStructure } from './lib.mjs'
export default async (page) => {
  await connect(page); await openStructure(page)
  await page.getByLabel('Foreign key 1 name').scrollIntoViewIfNeeded(); await page.waitForTimeout(500)
}
