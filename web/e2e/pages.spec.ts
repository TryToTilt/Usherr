import { expect, test } from '@playwright/test'
import { expectNoErrors, narrow, openNav, watchErrors } from './helpers'

// Every main page opens without an error on a new, empty install.
const PAGES: [string, RegExp][] = [
  ['/', /Dashboard/],
  ['/discover', /Discover/],
  ['/library', /Library/],
  ['/calendar', /Upcoming/],
  ['/wanted', /Upcoming/],
  ['/queue', /Activity/],
  ['/stats', /Statistics/],
  ['/settings/general', /General/],
  ['/settings/modules', /Media types/],
  ['/settings/media', /Library.*Folders/],
  ['/settings/naming', /File names and importing/],
  ['/settings/quality', /Quality/],
  ['/settings/automation', /Automatic searching/],
  ['/settings/indexers', /Indexers/],
  ['/settings/usenet', /Usenet/],
  ['/settings/torrents', /Torrents/],
  ['/settings/vpn', /VPN/],
  ['/settings/download-rules', /Download rules/],
  ['/settings/subtitles', /Subtitles/],
  ['/settings/metadata', /Movie and show info/],
  ['/settings/lists', /Import lists/],
  ['/settings/media-servers', /Media servers/],
  ['/settings/notifications', /Notifications/],
  ['/settings/other-apps', /Other apps/],
  ['/settings/profile', /Your profile/],
  ['/settings/security', /Security/],
  ['/settings/services', /Linked services/],
  ['/settings/accounts', /All accounts/],
  ['/settings/requests', /Requests and reports/],
  ['/settings/sign-in', /Ways to sign in/],
  ['/settings/backup', /Backup and restore/],
  ['/settings/system', /Server/],
  ['/settings/logs', /Logs and errors/],
  ['/settings/migrate', /Move from other apps/],
  ['/settings/about', /About and credits/],
  ['/help', /User guide/],
]

for (const [path, title] of PAGES) {
  test(`opens ${path}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto(path)
    await expect(page).toHaveTitle(title)
    await expect(page.getByText('Something went wrong')).toHaveCount(0)
    await expectNoErrors(errors)
  })
}

// Addresses that moved when the settings were split up still open.
const MOVED: [string, string][] = [
  ['/settings/downloads', '/settings/usenet'],
  ['/settings/language', '/settings/general'],
  ['/settings', '/settings/general'],
]
for (const [from, to] of MOVED) {
  test(`${from} opens ${to}`, async ({ page }) => {
    await page.goto(from)
    await expect(page).toHaveURL(new RegExp(`${to}$`))
  })
}

test('the Settings entry lists its groups once opened, and one group is open at a time', async ({ page }) => {
  await page.goto('/')
  await openNav(page)
  const nav = page.getByRole('navigation', { name: 'Main' })
  // Usherr: Settings folds away (to make room for your stack) until it's
  // opened; in a phone's or tablet's menu pressing it only unfolds it.
  const settings = nav.getByRole('link', { name: /^Settings/ }).first()
  await expect(nav.getByRole('button', { name: /Downloading/ })).toHaveCount(0)
  await settings.click()
  await expect(settings).toHaveAttribute('aria-expanded', 'true')
  const library = nav.getByRole('button', { name: /^Library$/ }).last()
  const downloading = nav.getByRole('button', { name: /Downloading/ })
  await expect(library).toBeVisible()
  await library.click()
  await expect(library).toHaveAttribute('aria-expanded', 'true')
  // a tap on a group also opens its first page, which closes the drawer on a phone
  await openNav(page)
  await downloading.click()
  await expect(downloading).toHaveAttribute('aria-expanded', 'true')
  await expect(library).toHaveAttribute('aria-expanded', 'false')
})

test('the Library entry lists its kinds, and each one opens the Library on that tab', async ({ page }) => {
  await page.goto('/')
  await openNav(page)
  const nav = page.getByRole('navigation', { name: 'Main' })
  // Usherr: the kinds fold up under Library until it's opened. On a wide
  // screen pressing Library also opens the Library; in a phone's or tablet's
  // menu it only unfolds the kinds, so the menu stays open.
  await nav.getByRole('link', { name: /^Library/ }).first().click()
  if (!narrow(page)) await expect(page).toHaveURL(/\/library/)
  const movies = nav.getByRole('link', { name: 'Movies', exact: true })
  const shows = nav.getByRole('link', { name: 'TV shows', exact: true })
  await expect(movies).toBeVisible()
  await shows.click()
  await expect(page).toHaveURL(/\/library\?kind=tv$/)
  await expect(shows).toHaveAttribute('aria-current', 'page')
  await expect(movies).not.toHaveAttribute('aria-current', 'page')
  await openNav(page)
  await movies.click()
  await expect(page).toHaveURL(/\/library\?kind=movie$/)
  await expect(movies).toHaveAttribute('aria-current', 'page')
})

test('an unknown address shows the not-found page', async ({ page }) => {
  await page.goto('/no-such-page')
  await expect(page.getByRole('heading', { level: 1 }).filter({ hasText: /exist|found/i })).toBeVisible()
})
