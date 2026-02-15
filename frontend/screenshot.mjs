#!/usr/bin/env node
import { chromium } from '@playwright/test';
import { spawn } from 'child_process';

async function takeScreenshots() {
  console.log('Starting dev server...');
  const devServer = spawn('npm', ['run', 'dev'], {
    cwd: process.cwd(),
    stdio: 'pipe',
  });

  // Wait for server to be ready
  await new Promise((resolve) => {
    devServer.stdout.on('data', (data) => {
      if (data.toString().includes('Local:')) {
        console.log('Dev server ready!');
        setTimeout(resolve, 2000); // Extra wait for stability
      }
    });
  });

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1280, height: 720 },
  });

  const page = await context.newPage();

  console.log('Taking screenshots...');

  // Screenshot 1: VOD List
  await page.goto('http://localhost:5173');
  await page.waitForSelector('table', { timeout: 10000 });
  await page.screenshot({ path: '/tmp/vod-list.png', fullPage: true });
  console.log('1. VOD List saved as /tmp/vod-list.png');

  // Screenshot 2: VOD Detail
  await page.getByText('Test VOD 1').click();
  await page.waitForSelector('text=Back to list', { timeout: 5000 });
  await page.screenshot({ path: '/tmp/vod-detail.png', fullPage: true });
  console.log('2. VOD Detail saved as /tmp/vod-detail.png');

  // Screenshot 3: Skip to content link (focus state)
  await page.goto('http://localhost:5173');
  await page.keyboard.press('Tab');
  await page.screenshot({ path: '/tmp/skip-link-focus.png' });
  console.log('3. Skip link focus state saved as /tmp/skip-link-focus.png');

  await browser.close();
  devServer.kill();

  console.log('Screenshots complete!');
  process.exit(0);
}

takeScreenshots().catch((error) => {
  console.error('Error taking screenshots:', error);
  process.exit(1);
});

