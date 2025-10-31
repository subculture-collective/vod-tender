import { describe, it, expect, vi } from 'vitest'
import { render, screen, waitFor } from '../test/utils'
import { server } from '../test/setup'
import { http, HttpResponse } from 'msw'
import VodList from './VodList'

describe('VodList', () => {
  it('renders loading state initially', () => {
    render(<VodList />)
    expect(screen.getByText(/loading vods/i)).toBeInTheDocument()
  })

  it('renders VOD list after loading', async () => {
    render(<VodList />)

    // Wait for loading to complete
    await waitFor(() => {
      expect(screen.queryByText(/loading vods/i)).not.toBeInTheDocument()
    })

    // Check if VODs are rendered
    expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    expect(screen.getByText('Test VOD 2')).toBeInTheDocument()
  })

  it('displays processed status correctly', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Check processed status
    const processedElements = screen.getAllByText('Processed')
    const pendingElements = screen.getAllByText('Pending')

    expect(processedElements.length).toBeGreaterThan(0)
    expect(pendingElements.length).toBeGreaterThan(0)
  })

  it('displays YouTube links when available', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Check for YouTube link
    const youtubeLinks = screen.getAllByRole('link', { name: /youtube/i })
    expect(youtubeLinks.length).toBeGreaterThan(0)

    // First link should be for Test VOD 1
    expect(youtubeLinks[0]).toHaveAttribute(
      'href',
      'https://youtube.com/watch?v=test1'
    )
  })

  it('displays error message on API failure', async () => {
    // Override MSW handler to return error
    server.use(
      http.get('/vods', () => {
        return new HttpResponse(null, { status: 500 })
      })
    )

    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText(/failed to fetch vods/i)).toBeInTheDocument()
    })
  })

  it('calls onVodSelect when a VOD is clicked', async () => {
    const onVodSelect = vi.fn()
    render(<VodList onVodSelect={onVodSelect} />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Click on the first VOD row
    const vodRow = screen.getByText('Test VOD 1').closest('tr')
    expect(vodRow).toBeInTheDocument()

    vodRow?.click()

    expect(onVodSelect).toHaveBeenCalledWith('1')
  })

  it('displays empty state correctly', async () => {
    // Override MSW handler to return empty array
    server.use(
      http.get('/vods', () => {
        return HttpResponse.json([])
      })
    )

    render(<VodList />)

    await waitFor(() => {
      expect(screen.queryByText(/loading vods/i)).not.toBeInTheDocument()
    })

    // Table should be rendered but with no rows
    expect(screen.getByRole('table')).toBeInTheDocument()
    const tableBody = screen.getByRole('table').querySelector('tbody')
    expect(tableBody?.children.length).toBe(0)
  })

  it('formats dates correctly', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Check that dates are formatted (we can't check exact format due to locale differences)
    const dateElements = screen.getAllByText(/2025/i)
    expect(dateElements.length).toBeGreaterThan(0)
  })

  it('renders without onVodSelect callback', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Should still render the table
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  it('stops propagation when clicking YouTube link', async () => {
    const onVodSelect = vi.fn()
    render(<VodList onVodSelect={onVodSelect} />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Click on YouTube link
    const youtubeLink = screen.getAllByText('YouTube')[0]
    youtubeLink.click()

    // onVodSelect should not be called when clicking YouTube link
    expect(onVodSelect).not.toHaveBeenCalled()
  })

  it('renders pagination controls', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Check for pagination buttons
    expect(screen.getByText('Previous')).toBeInTheDocument()
    expect(screen.getByText('Next')).toBeInTheDocument()
    expect(screen.getByText('Page 1')).toBeInTheDocument()
  })

  it('disables Previous button on first page', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    const previousButton = screen.getByText('Previous')
    expect(previousButton).toBeDisabled()
  })

  it('disables Next button when fewer items than limit', async () => {
    render(<VodList />)

    await waitFor(() => {
      expect(screen.getByText('Test VOD 1')).toBeInTheDocument()
    })

    // Since we only have 2 VODs (less than limit of 50), Next should be disabled
    const nextButton = screen.getByText('Next')
    expect(nextButton).toBeDisabled()
  })
})
