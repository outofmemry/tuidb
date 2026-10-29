import { connect } from './lib.mjs'
export default async (page) => {
  await connect(page)
  await page.getByRole('button', { name: 'Create' }).click(); await page.waitForTimeout(500)
}
