import { connect, pick } from './lib.mjs'
export default async (page) => {
  await connect(page)
  await page.keyboard.press('Control+d'); await page.keyboard.press('m'); await page.waitForTimeout(1500)
  await page.getByRole('button', { name: 'New board…' }).click(); await page.waitForTimeout(600)
  await page.getByLabel('Board name').fill('Customers')
  await page.getByLabel('Panel title').fill('Sign-ups per month')
  await pick(page, 'Kind', 'bar')
  await page.locator('textarea[aria-label="Query"]').fill("SELECT to_char(signed_up, 'YYYY-MM') AS month, count(*) AS signups FROM customers GROUP BY 1 ORDER BY 1")
  await page.getByLabel('Chart columns').fill('x: month, y: signups'); await page.waitForTimeout(400)
}
