# Frontend Testing Guide

This document describes the frontend testing infrastructure for the vod-tender project.

## Overview

The frontend uses a comprehensive testing strategy with three types of tests:

1. **Unit Tests** - Test individual functions and utilities
2. **Component Tests** - Test React components in isolation
3. **E2E Tests** - Test full user workflows with Playwright

## Tech Stack

- **Vitest** - Fast unit test framework with native ESM support
- **React Testing Library** - Component testing utilities
- **MSW** (Mock Service Worker) - API mocking for tests
- **Playwright** - Cross-browser E2E testing
- **@axe-core/playwright** - Accessibility testing
- **Prettier** - Code formatting
- **Husky + lint-staged** - Pre-commit hooks

## Running Tests

### Unit & Component Tests

```bash
# Run tests in watch mode (default)
npm test

# Run tests once
npm test -- --run

# Run tests with coverage
npm run test:coverage

# Run tests with UI
npm run test:ui
```

### E2E Tests

```bash
# Install Playwright browsers (first time only)
npm run playwright:install

# Run E2E tests
npm run test:e2e

# Run E2E tests with UI
npm run test:e2e:ui

# Run E2E tests in debug mode
npm run test:e2e:debug
```

## Test Structure

### Unit Tests

Located in `src/lib/` alongside the code they test:

- `src/lib/api.test.ts` - Tests for API utility functions

### Component Tests

Located in `src/components/` alongside the components:

- `src/components/VodList.test.tsx` - Tests for VOD list component
- `src/components/VodDetail.test.tsx` - Tests for VOD detail component
- `src/components/ChatReplay.test.tsx` - Tests for chat replay component
- `src/App.test.tsx` - Integration tests for main app

### E2E Tests

Located in `e2e/` directory:

- `e2e/vod-list.spec.ts` - Tests for VOD list page workflows
- `e2e/vod-detail.spec.ts` - Tests for VOD detail page workflows
- `e2e/accessibility.spec.ts` - Accessibility compliance tests

### Test Utilities

- `src/test/setup.ts` - Global test setup with MSW handlers
- `src/test/utils.tsx` - Custom render utilities and test helpers

## Writing Tests

### Component Test Example

```typescript
import { describe, it, expect } from 'vitest'
import { render, screen, waitFor } from '../test/utils'
import { server } from '../test/setup'
import { http, HttpResponse } from 'msw'
import MyComponent from './MyComponent'

describe('MyComponent', () => {
  it('renders data after loading', async () => {
    render(<MyComponent />)

    await waitFor(() => {
      expect(screen.getByText('Expected Text')).toBeInTheDocument()
    })
  })

  it('handles API errors', async () => {
    // Override MSW handler for this test
    server.use(
      http.get('/api/endpoint', () => {
        return new HttpResponse(null, { status: 500 })
      })
    )

    render(<MyComponent />)

    await waitFor(() => {
      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })
  })
})
```

### E2E Test Example

```typescript
import { test, expect } from '@playwright/test'

test.describe('Feature Name', () => {
  test('accomplishes user goal', async ({ page }) => {
    await page.goto('/')

    await expect(page.getByRole('heading')).toBeVisible()

    await page.getByRole('button', { name: /click me/i }).click()

    await expect(page.getByText(/success/i)).toBeVisible()
  })
})
```

## Coverage Requirements

Test coverage thresholds are enforced in CI:

- **Lines**: 80%
- **Functions**: 65%
- **Branches**: 70%
- **Statements**: 80%

Current coverage:
- **Overall**: 93.79% statements, 90.62% branches, 68.75% functions

## MSW API Mocking

API handlers are defined in `src/test/setup.ts`:

```typescript
export const handlers = [
  http.get('/vods', () => {
    return HttpResponse.json([
      { id: '1', title: 'Test VOD', date: '2025-10-19T10:00:00Z' }
    ])
  }),
]
```

Override handlers in individual tests:

```typescript
import { server } from '../test/setup'
import { http, HttpResponse } from 'msw'

test('specific scenario', async () => {
  server.use(
    http.get('/vods', () => {
      return HttpResponse.json([])
    })
  )
  // ... rest of test
})
```

## Pre-commit Hooks

Pre-commit hooks automatically run on `git commit`:

1. **ESLint** - Fixes linting issues
2. **Prettier** - Formats code
3. **Type checking** - Validates TypeScript

Configure in `package.json` under `lint-staged`.

## CI/CD Integration

Tests run automatically on pull requests:

### Frontend Job
- TypeScript type checking
- ESLint linting
- Prettier format check
- Unit tests with coverage
- Build verification
- Coverage upload to Codecov

### Frontend E2E Job
- Playwright E2E tests (Chromium only)
- Accessibility tests with axe-core
- Test reports uploaded as artifacts

## Accessibility Testing

Accessibility tests use axe-core to scan for violations:

```typescript
import AxeBuilder from '@axe-core/playwright'

test('page has no accessibility violations', async ({ page }) => {
  await page.goto('/')
  
  const results = await new AxeBuilder({ page }).analyze()
  
  expect(results.violations).toEqual([])
})
```

## Best Practices

### Component Tests

1. **Test user behavior, not implementation**
   - Use `screen.getByRole()` over `getByTestId()`
   - Test what users see and interact with

2. **Use `waitFor()` for async behavior**
   - Always wait for async state updates
   - Avoid `act()` warnings by wrapping updates

3. **Mock external dependencies**
   - Use MSW for API calls
   - Mock complex child components if needed

### E2E Tests

1. **Test critical user flows only**
   - Happy paths and error scenarios
   - Don't duplicate component test coverage

2. **Use semantic selectors**
   - Prefer `getByRole()` and `getByLabel()`
   - Avoid brittle selectors like classes

3. **Keep tests independent**
   - Each test should work in isolation
   - Don't rely on test execution order

### General

1. **Follow the testing pyramid**
   - More unit tests, fewer E2E tests
   - Unit: 48 tests, E2E: 21 tests

2. **Keep tests readable**
   - Clear test names describing behavior
   - Arrange-Act-Assert pattern

3. **Maintain test hygiene**
   - Remove obsolete tests promptly
   - Update tests when behavior changes

## Debugging Tests

### Unit Tests

```bash
# Run specific test file
npm test -- src/components/VodList.test.tsx

# Run tests matching pattern
npm test -- --grep "renders VOD list"

# Run with UI for debugging
npm run test:ui
```

### E2E Tests

```bash
# Run in headed mode
npx playwright test --headed

# Run in debug mode with inspector
npm run test:e2e:debug

# Run specific test file
npx playwright test e2e/vod-list.spec.ts
```

## Troubleshooting

### "MSW handler not being called"

Make sure the URL in the handler matches exactly:
```typescript
// Correct - matches fetch('/vods')
http.get('/vods', () => ...)

// Wrong - won't match
http.get('http://localhost:3000/vods', () => ...)
```

### "act() warning in tests"

Wrap state updates in `waitFor()`:
```typescript
await waitFor(() => {
  expect(screen.getByText('Updated')).toBeInTheDocument()
})
```

### "Playwright test timing out"

Increase timeout or check for hung processes:
```typescript
test('slow test', async ({ page }) => {
  test.setTimeout(60000) // 60 seconds
  // ... rest of test
})
```

## Resources

- [Vitest Documentation](https://vitest.dev)
- [React Testing Library](https://testing-library.com/react)
- [Playwright Documentation](https://playwright.dev)
- [MSW Documentation](https://mswjs.io)
- [Testing Library Best Practices](https://kentcdodds.com/blog/common-mistakes-with-react-testing-library)
