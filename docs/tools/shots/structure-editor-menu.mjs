import { connect, openStructure } from './lib.mjs'
export default async (page) => {
  await connect(page); await openStructure(page)
  await page.getByLabel('Column 6 name').click({ button: 'right' }); await page.waitForTimeout(600)
}
