import { describe, it, expect } from 'vitest'
import { render, screen } from '../test/utils'
import { Skeleton, VodTableSkeleton } from './Skeleton'

describe('Skeleton', () => {
  it('renders with default className', () => {
    const { container } = render(<Skeleton />)
    const skeleton = container.querySelector('div')
    expect(skeleton).toHaveClass('animate-pulse', 'bg-gray-300', 'rounded')
  })

  it('renders with custom className', () => {
    const { container } = render(<Skeleton className="h-10 w-20" />)
    const skeleton = container.querySelector('div')
    expect(skeleton).toHaveClass('h-10', 'w-20')
  })

  it('has accessibility attributes', () => {
    render(<Skeleton />)
    const skeleton = screen.getByRole('generic', { busy: true })
    expect(skeleton).toHaveAttribute('aria-busy', 'true')
    expect(skeleton).toHaveAttribute('aria-live', 'polite')
  })
})

describe('VodTableSkeleton', () => {
  it('renders table structure with skeleton rows', () => {
    render(<VodTableSkeleton />)

    // Check table exists
    expect(screen.getByRole('table')).toBeInTheDocument()

    // Check headers are present
    expect(screen.getByText('Title')).toBeInTheDocument()
    expect(screen.getByText('Date')).toBeInTheDocument()
    expect(screen.getByText('Status')).toBeInTheDocument()
    expect(screen.getByText('YouTube')).toBeInTheDocument()

    // Check for skeleton elements (should have multiple)
    const skeletons = screen.getAllByRole('generic', { busy: true })
    expect(skeletons.length).toBeGreaterThan(5) // At least 5 rows + title skeleton
  })

  it('renders 5 skeleton rows', () => {
    const { container } = render(<VodTableSkeleton />)
    const tbody = container.querySelector('tbody')
    const rows = tbody?.querySelectorAll('tr')
    expect(rows?.length).toBe(5)
  })
})
