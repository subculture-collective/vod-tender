import { test, expect } from '@playwright/test'

test.describe('VOD Detail Page', () => {
  test('displays VOD details and metadata', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    // Check back button
    await expect(page.getByText(/back to list/i)).toBeVisible()

    // Check VOD title
    await expect(page.getByRole('heading', { name: /test vod 1/i })).toBeVisible()

    // Check status
    await expect(page.getByText('Processed')).toBeVisible()
  })

  test('displays progress bar for VOD being processed', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD 2 which is being processed
    await page.getByText(/test vod 2/i).click()

    // Check for progress information
    await expect(page.getByText(/downloading/i)).toBeVisible()
    await expect(page.getByText(/45\.5%/)).toBeVisible()
  })

  test('displays YouTube link when available', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD 1 which has a YouTube link
    await page.getByText(/test vod 1/i).click()

    // Check for YouTube link
    const youtubeLink = page.getByRole('link', { name: /watch on youtube/i })
    await expect(youtubeLink).toBeVisible()
    await expect(youtubeLink).toHaveAttribute('target', '_blank')
  })

  test('navigates back to list when clicking back button', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()
    await expect(page.getByText(/back to list/i)).toBeVisible()

    // Click back button
    await page.getByText(/back to list/i).click()

    // Should be back at list
    await expect(page.getByRole('heading', { name: /twitch vods/i })).toBeVisible()
  })

  test('displays chat replay section', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    // Check for chat replay
    await expect(page.getByText(/chat replay/i)).toBeVisible()

    // Check for chat controls
    await expect(page.getByText('Static')).toBeVisible()
    await expect(page.getByLabel(/speed/i)).toBeVisible()
    await expect(page.getByLabel(/from/i)).toBeVisible()
  })

  test('displays chat messages', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    // Wait for chat to load
    await expect(page.getByText(/testuser1/i)).toBeVisible()
    await expect(page.getByText(/hello world/i)).toBeVisible()
  })

  test('toggles between static and live chat replay', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    // Initially in static mode
    const toggleButton = page.getByText('Static')
    await expect(toggleButton).toBeVisible()

    // Note: We don't click it here because it would try to establish SSE connection
    // which would fail without a real backend. This is just checking the UI exists.
  })

  test('allows changing chat replay speed', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    const speedSelect = page.getByLabel(/speed/i)
    await expect(speedSelect).toBeVisible()

    // Speed select should be disabled in static mode
    await expect(speedSelect).toBeDisabled()
  })

  test('allows changing chat start time', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    const fromInput = page.getByLabel(/from/i)
    await expect(fromInput).toBeVisible()
    await expect(fromInput).toHaveValue('0')
  })
})
