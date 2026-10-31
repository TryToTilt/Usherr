import { expect, test } from '@playwright/test'
import { expectNoErrors, watchErrors } from './helpers'

// Usherr: Show Me How's self-check in a real browser. The menu lists the
// walkthroughs, and every one of them starts: the coach card shows its first
// step. (tools/check-smh.mjs checks, before each build, that every button a
// walkthrough points at still exists.)
test('every Show me how walkthrough starts', async ({ page }) => {
  test.setTimeout(300_000)
  const errors = watchErrors(page)
  await page.goto('/')
  await page.locator('[data-walk="smh-button"]').click()
  await expect(page.locator('.smh-item').first()).toBeVisible()
  const names = await page.locator('.smh-item-text b').allInnerTexts()
  expect(names.length).toBeGreaterThan(20)
  for (let i = 0; i < names.length; i++) {
    await page.goto('/')
    await page.locator('[data-walk="smh-button"]').click()
    await page.locator('.smh-item').nth(i).getByRole('button', { name: 'Simple' }).click()
    const card = page.locator('.smh-coach')
    await expect(card, names[i]).toBeVisible()
    await expect(card.locator('h2'), names[i]).not.toBeEmpty()
    // Put it away: pause it, then forget it from the menu.
    await card.locator('.smh-x').click()
    await page.keyboard.press('Escape') // a window the walkthrough opened (Roulette, Tonight)
    await page.goto('/')
    await page.locator('[data-walk="smh-button"]').click()
    await page.locator('.smh-resume').getByRole('button', { name: 'Forget it' }).click()
    await page.keyboard.press('Escape')
  }
  await expectNoErrors(errors)
})

test('the Doctor checks Usherr', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/')
  await page.locator('[data-walk="smh-button"]').click()
  await page.locator('.smh-doctor').click()
  await expect(page.locator('.doc-sum')).toBeVisible({ timeout: 20_000 })
  await expect(page.locator('.doc-parts li').first()).toBeVisible()
  await expectNoErrors(errors)
})
