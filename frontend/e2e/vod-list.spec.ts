import { test, expect } from '@playwright/test'

test.describe('VOD List Page', () => {
  test('displays the header and VOD list', async ({ page }) => {
    await page.goto('/')

    // Check header
    await expect(
      page.getByRole('heading', { name: /vod tender dashboard/i })
    ).toBeVisible()

    // Check VOD list heading
    await expect(
      page.getByRole('heading', { name: /twitch vods/i })
    ).toBeVisible()

    // Check for table
    const table = page.locator('table')
    await expect(table).toBeVisible()
  })

  test('displays VOD information in table', async ({ page }) => {
    await page.goto('/')

    // Wait for VODs to load
    await expect(page.getByText(/test vod/i).first()).toBeVisible()

    // Check table headers
    await expect(page.getByRole('columnheader', { name: /title/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /date/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /status/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /youtube/i })).toBeVisible()
  })

  test('navigates to VOD detail when clicking on a row', async ({ page }) => {
    await page.goto('/')

    // Wait for VODs to load
    await expect(page.getByText(/test vod 1/i)).toBeVisible()

    // Click on the first VOD row
    await page.getByText(/test vod 1/i).click()

    // Should navigate to detail page
    await expect(page.getByText(/back to list/i)).toBeVisible()
    await expect(page.getByText(/chat replay/i)).toBeVisible()
  })

  test('displays processed and pending statuses', async ({ page }) => {
    await page.goto('/')

    // Wait for VODs to load
    await expect(page.getByText(/test vod/i).first()).toBeVisible()

    // Check for status indicators
    const processedStatus = page.getByText('Processed').first()
    const pendingStatus = page.getByText('Pending').first()

    await expect(processedStatus).toBeVisible()
    await expect(pendingStatus).toBeVisible()
  })

  test('displays YouTube links for processed VODs', async ({ page }) => {
    await page.goto('/')

    // Wait for VODs to load
    await expect(page.getByText(/test vod 1/i)).toBeVisible()

    // Check for YouTube link
    const youtubeLink = page.getByRole('link', { name: /youtube/i }).first()
    await expect(youtubeLink).toBeVisible()
    await expect(youtubeLink).toHaveAttribute('href', /youtube\.com/)
  })

  test('displays footer with copyright', async ({ page }) => {
    await page.goto('/')

    const currentYear = new Date().getFullYear()
    await expect(page.getByText(new RegExp(currentYear.toString()))).toBeVisible()
  })
})
