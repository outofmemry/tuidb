import { connect } from './lib.mjs'
export default async (page) => {
  await connect(page)
  await page.getByRole('button', { name: 'Create' }).click(); await page.waitForTimeout(400)
  await page.getByRole('menuitem', { name: /New table/ }).click(); await page.waitForTimeout(500)
  await page.getByRole('textbox').last().fill('docs_structure_invoices'); await page.waitForTimeout(300)
}
