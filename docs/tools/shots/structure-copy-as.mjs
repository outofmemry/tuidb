import { connect, openStructure } from './lib.mjs'
export default async (page) => {
  await connect(page); await openStructure(page)
  await page.getByRole('button', { name: 'Copy as…' }).click(); await page.waitForTimeout(600)
}
