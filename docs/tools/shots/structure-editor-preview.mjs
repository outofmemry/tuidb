import { connect, openStructure, pendingEdits } from './lib.mjs'
export default async (page) => {
  await connect(page); await openStructure(page); await pendingEdits(page)
  await page.getByRole('button', { name: 'Preview changes' }).click(); await page.waitForTimeout(1500)
}
