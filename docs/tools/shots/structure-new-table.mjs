import { connect } from './lib.mjs'
export default async (page) => {
  await connect(page)
  await page.getByRole('button', { name: 'Create' }).click(); await page.waitForTimeout(400)
  await page.getByRole('menuitem', { name: /New table/ }).click(); await page.waitForTimeout(500)
  await page.getByRole('textbox').last().fill('docs_structure_invoices'); await page.keyboard.press('Enter'); await page.waitForTimeout(1200)
  for (const [i, n, t] of [[2, 'customer_id', 'integer'], [3, 'amount', 'numeric(10,2)']]) {
    await page.getByRole('button', { name: 'Add column', exact: true }).click(); await page.waitForTimeout(250)
    await page.getByLabel(`Column ${i} name`).fill(n); await page.getByLabel(`Column ${i} type`).fill(t)
  }
  await page.waitForTimeout(300)
}
