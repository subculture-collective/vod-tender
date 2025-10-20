import { render } from '@testing-library/react'
import type { RenderOptions } from '@testing-library/react'
import type { ReactElement } from 'react'

/**
 * Custom render function that wraps components with any necessary providers
 */
function customRender(
  ui: ReactElement,
  options?: Omit<RenderOptions, 'wrapper'>
) {
  return render(ui, { ...options })
}

export * from '@testing-library/react'
export { customRender as render }
