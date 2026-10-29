export async function connect(page, name = 'Shop (Postgres)') {
  for (let i = 0; i < 3; i++) {
    await page.getByText(name, { exact: true }).first().click().catch(() => {})
    try { await page.locator('[role=treeitem]').first().waitFor({ state: 'visible', timeout: 12000 }); await page.waitForTimeout(800); return } catch {}
    await page.goto(page.url().split('#')[0] + '#token=devtoken', { waitUntil: 'networkidle' }).catch(() => {})
  }
  throw new Error('could not connect')
}
export async function openTable(page, name) {
  await page.getByRole('treeitem', { name: new RegExp('^' + name) }).first().click()
  await page.locator('[role=row]').nth(2).waitFor({ state: 'visible', timeout: 10000 })
  await page.waitForTimeout(600)
}
// Pick an option of a custom Select by its aria-label.
export async function pick(page, label, text, n = 0) {
  await page.locator(`[aria-label="${label}"]`).nth(n).click()
  await page.waitForTimeout(250)
  await page.getByRole('menuitemradio', { name: text, exact: true }).first().click()
  await page.waitForTimeout(250)
}
// Open the filter bar on the first filter row.
export async function filterBar(page) {
  await page.getByRole('button', { name: 'Filters' }).click(); await page.waitForTimeout(500)
}
// Open the Structure view of a table (C-d ) from the grid).
export async function openStructure(page, table = 'orders') {
  await openTable(page, table)
  await page.locator('[role=row] >> nth=3 >> [data-c="1"]').click()
  await page.keyboard.press('Control+d'); await page.keyboard.press(')')
  await page.getByRole('button', { name: 'Preview changes' }).waitFor({ state: 'visible', timeout: 8000 })
  await page.waitForTimeout(600)
}
// The three edits of structure-editor-pending: coupon -> coupon_code, total -> numeric(12,2), new column notes (text).
export async function pendingEdits(page) {
  await page.getByLabel('Column 8 name').fill('coupon_code')
  await page.getByLabel('Column 6 type').fill('numeric(12,2)')
  await page.getByRole('button', { name: 'Add column', exact: true }).click(); await page.waitForTimeout(300)
  await page.getByLabel('Column 9 name').fill('notes')
  await page.getByLabel('Column 9 type').fill('text')
  await page.waitForTimeout(300)
}
