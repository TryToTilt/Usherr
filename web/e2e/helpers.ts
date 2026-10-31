import { expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

export const AUTH_FILE = 'e2e/.auth/admin.json'
export const CREDS_FILE = 'e2e/.auth/creds.json'

// The administrator the setup test created.
export function creds(): { username: string; password: string } {
  return JSON.parse(readFileSync(CREDS_FILE, 'utf8'))
}

// Collects uncaught errors in the page, so a test can fail on them.
export function watchErrors(page: Page): string[] {
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  return errors
}

export async function expectNoErrors(errors: string[]) {
  expect(errors, errors.join('\n')).toEqual([])
}

// The layout for tablets and phones starts at 900px: the main menu is a drawer
// that starts closed (the theme button and the links to the guide live in it)
// and the guide's landing page lists its topics as cards instead of a side list.
export const narrow = (page: Page) => (page.viewportSize()?.width ?? 1280) <= 900

// The menu's own button (the menu's close button shares its class).
export const menuButton = (page: Page) => page.locator('.menu-button[aria-controls="main-menu"]:not(.menu-close)')

// Opens the menu. Usherr: it's closed until opened on every screen, and on a
// wide one it slides away once the mouse leaves it, so the mouse rests on it.
export async function openNav(page: Page) {
  const button = menuButton(page)
  await expect(button).toBeVisible()
  if ((await button.getAttribute('aria-expanded')) !== 'true') await button.click()
  const nav = page.locator('#main-menu')
  await expect(nav).toHaveClass(/open/)
  if (!narrow(page)) await nav.hover()
}
