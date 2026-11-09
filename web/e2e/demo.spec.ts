import { expect, test } from '@playwright/test'
import { expectNoErrors, watchErrors } from './helpers'

// Demo mode fills the screens with made-up titles; nothing is saved.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('mediarium-demo', '1'))
})

test('the library, a title page and the queue show the sample titles', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/library?kind=movie')
  await expect(page.getByText('Saltwind: Part Two').first()).toBeVisible()
  await page.getByText('The Long Equation').first().click()
  await expect(page.getByRole('heading', { name: /The Long Equation/ }).first()).toBeVisible()
  await page.goto('/queue')
  await expect(page.getByText('Northbound').first()).toBeVisible()
  await expectNoErrors(errors)
})

test('discover shows only made-up titles', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/discover?kind=movie')
  await expect(page.getByText('Small Weather').first()).toBeVisible()
  await expectNoErrors(errors)
})

test('a cast member opens the person page with their work', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/library?kind=movie')
  await page.getByText('The Long Equation').first().click()
  await expect(page.getByRole('heading', { name: /The Long Equation/ }).first()).toBeVisible()
  // Usherr: on a phone or tablet the film page is in tabs (the Cinema layout), the cast behind Cast.
  const castTab = page.getByRole('tab', { name: 'Cast' })
  if (await castTab.count()) await castTab.click()
  await page.getByRole('link', { name: /Actor One/ }).first().click()
  await expect(page).toHaveURL(/\/person\/9001$/)
  await expect(page.getByRole('heading', { name: 'Actor One' }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Movies' })).toBeVisible()
  await page.getByRole('combobox', { name: 'Kind of work' }).selectOption('directing') // Usherr: a menu, with sort and filters beside it
  await expect(page).toHaveURL(/role=directing/)
  await expectNoErrors(errors)
})

// Usherr Broadcast: Home is the sofa's Home; a title's page has one main
// button then Stack, Trailer and More, and the remote's Options key (M)
// opens the same More sheet.
test('Broadcast: Home, and a title page with one main button and a More sheet', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/')
  test.skip(!(await page.evaluate(() => document.documentElement.dataset.look === 'broadcast')), 'Broadcast only')
  await expect(page.locator('.bh-home')).toBeVisible()
  await page.goto('/library?kind=movie')
  await page.getByText('The Long Equation').first().click()
  await expect(page.getByRole('heading', { name: /The Long Equation/ }).first()).toBeVisible()
  const actions = page.locator('.bc-title-actions')
  await expect(actions).toBeVisible()
  await expect(actions.locator('[data-autofocus]')).toHaveCount(1)
  await actions.getByRole('button', { name: 'More', exact: true }).click()
  const sheet = page.locator('.bc-sheet')
  await expect(sheet).toBeVisible()
  await expect(sheet.getByRole('button', { name: /Book a showtime/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(sheet).toHaveCount(0)
  await page.keyboard.press('m')
  await expect(sheet).toBeVisible()
  await page.keyboard.press('m')
  await expect(sheet).toHaveCount(0)
  await expectNoErrors(errors)
})
