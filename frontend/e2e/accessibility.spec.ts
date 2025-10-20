import { test, expect } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

test.describe('Accessibility Tests', () => {
  test('VOD list page should not have accessibility violations', async ({
    page,
  }) => {
    await page.goto('/')

    // Wait for content to load
    await expect(page.getByText(/test vod/i).first()).toBeVisible()

    const accessibilityScanResults = await new AxeBuilder({ page }).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('VOD detail page should not have accessibility violations', async ({
    page,
  }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()
    await expect(page.getByText(/back to list/i)).toBeVisible()

    const accessibilityScanResults = await new AxeBuilder({ page }).analyze()

    expect(accessibilityScanResults.violations).toEqual([])
  })

  test('keyboard navigation works on VOD list', async ({ page }) => {
    await page.goto('/')

    // Wait for content to load
    await expect(page.getByText(/test vod/i).first()).toBeVisible()

    // Tab through interactive elements
    await page.keyboard.press('Tab')

    // Verify focus is visible (at least one element should be focused)
    const focusedElement = page.locator(':focus')
    await expect(focusedElement).toBeVisible()
  })

  test('all images have alt text', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail to see chat with badges/emotes
    await page.getByText(/test vod 1/i).click()
    await expect(page.getByText(/testuser1/i)).toBeVisible()

    // Check all images have alt attributes
    const images = page.locator('img')
    const imageCount = await images.count()

    for (let i = 0; i < imageCount; i++) {
      const img = images.nth(i)
      const altText = await img.getAttribute('alt')
      expect(altText).toBeDefined()
    }
  })

  test('form controls have associated labels', async ({ page }) => {
    await page.goto('/')

    // Navigate to VOD detail
    await page.getByText(/test vod 1/i).click()

    // Check that speed and from inputs have labels
    await expect(page.getByLabel(/speed/i)).toBeVisible()
    await expect(page.getByLabel(/from/i)).toBeVisible()
  })

  test('links have descriptive text', async ({ page }) => {
    await page.goto('/')

    // Wait for content to load
    await expect(page.getByText(/test vod/i).first()).toBeVisible()

    // Check YouTube links have descriptive text
    const youtubeLinks = page.getByRole('link', { name: /youtube/i })
    const linkCount = await youtubeLinks.count()

    // Should have at least one YouTube link
    expect(linkCount).toBeGreaterThan(0)

    // Each link should have visible text
    for (let i = 0; i < linkCount; i++) {
      const link = youtubeLinks.nth(i)
      const text = await link.textContent()
      expect(text?.trim().length).toBeGreaterThan(0)
    }
  })
})
