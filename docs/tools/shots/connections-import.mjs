// Two shots from one run: the Import dialog (the --out file of the run) is taken mid-way, the result is the final shot.
// Run it against a throwaway server (serve-copy.sh): the import adds connections.
export default async (page, { shot }) => {
  await page.waitForTimeout(1000)
  await page.click('role=button[name="Import…"]')
  await page.waitForTimeout(300)
  await page.setInputFiles('input[type=file]', new URL('./scratch/shop.wtdb', import.meta.url).pathname)
  await page.fill('role=textbox[name="Passphrase"]', 'pw')
  await page.waitForTimeout(200)
  await shot(new URL('../../static/img/web/connections-import.png', import.meta.url).pathname)
  await page.click('role=button[name="Import"]')
  await page.waitForTimeout(1500)
}
