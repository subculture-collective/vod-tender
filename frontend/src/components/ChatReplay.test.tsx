import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '../test/utils'
import { server } from '../test/setup'
import { http, HttpResponse } from 'msw'
import ChatReplay from './ChatReplay'

describe('ChatReplay', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders loading state initially', () => {
    render(<ChatReplay vodId="1" />)
    expect(screen.getByText(/loading chat/i)).toBeInTheDocument()
  })

  it('renders chat messages after loading', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
      expect(screen.getByText('Hello world!')).toBeInTheDocument()
    })

    expect(screen.getByText('testuser2:')).toBeInTheDocument()
    expect(screen.getByText('Great stream!')).toBeInTheDocument()
  })

  it('displays user badges', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Check for badge images (broadcaster and subscriber)
    const badges = screen.getAllByRole('img', { name: /broadcaster|subscriber/i })
    expect(badges.length).toBeGreaterThan(0)
  })

  it('displays relative timestamps', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Check for timestamp display (300s and 360s)
    expect(screen.getByText(/300\.0s/)).toBeInTheDocument()
    expect(screen.getByText(/360\.0s/)).toBeInTheDocument()
  })

  it('applies user colors to usernames', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    const username1 = screen.getByText('testuser1:')
    expect(username1).toHaveStyle({ color: '#FF0000' })

    const username2 = screen.getByText('testuser2:')
    expect(username2).toHaveStyle({ color: '#00FF00' })
  })

  it('toggles between static and live replay mode', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Initially in static mode
    const toggleButton = screen.getByText('Static')
    expect(toggleButton).toBeInTheDocument()

    // Mock EventSource to prevent actual SSE connection
    const mockEventSource = {
      close: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
      onmessage: null,
      onerror: null,
      onopen: null,
      readyState: 0,
      url: '',
      withCredentials: false,
      CONNECTING: 0,
      OPEN: 1,
      CLOSED: 2,
    }
    vi.stubGlobal('EventSource', vi.fn(() => mockEventSource))

    // Click to switch to live mode
    await screen.findByText('Static')
    toggleButton.click()

    // Button text should change
    await waitFor(() => {
      expect(screen.getByText('Live')).toBeInTheDocument()
    })
  })

  it('allows changing playback speed', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    const speedSelect = screen.getByLabelText(/speed/i)
    expect(speedSelect).toBeInTheDocument()
    expect(speedSelect).toHaveValue('1')
  })

  it('allows changing start time with from parameter', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    const fromInput = screen.getByLabelText(/from/i)
    expect(fromInput).toBeInTheDocument()
    expect(fromInput).toHaveValue(0)
  })

  it('displays error message on API failure', async () => {
    // Override MSW handler to return error
    server.use(
      http.get('/vods/1/chat', () => {
        return new HttpResponse(null, { status: 500 })
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText(/unexpected end of json input/i)).toBeInTheDocument()
    })
  })

  it('renders empty chat when no messages', async () => {
    // Override MSW handler to return empty array
    server.use(
      http.get('/vods/1/chat', () => {
        return HttpResponse.json([])
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.queryByText(/loading chat/i)).not.toBeInTheDocument()
    })

    // Should have the chat container but no messages
    expect(screen.queryByText('testuser1:')).not.toBeInTheDocument()
  })

  it('disables speed select when not in replay mode', async () => {
    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    const speedSelect = screen.getByLabelText(/speed/i)
    
    // Should be disabled in static mode
    expect(speedSelect).toBeDisabled()
  })

  it('renders emote images when emotes are present', async () => {
    // Override handler to include emotes
    server.use(
      http.get('/vods/1/chat', () => {
        return HttpResponse.json([
          {
            username: 'testuser1',
            message: 'Hello Kappa world!',
            abs_timestamp: '2025-10-19T10:05:00Z',
            rel_timestamp: 300,
            badges: null,
            emotes: '25:6-10',
            color: '#FF0000',
          },
        ])
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Check for emote image
    const emoteImages = screen.getAllByRole('img').filter(img => 
      img.getAttribute('src')?.includes('emoticons')
    )
    expect(emoteImages.length).toBeGreaterThan(0)
  })

  it('respects from parameter in API request', async () => {
    let capturedFrom = 0
    
    server.use(
      http.get('/vods/1/chat', ({ request }) => {
        const url = new URL(request.url)
        capturedFrom = parseInt(url.searchParams.get('from') || '0', 10)
        
        return HttpResponse.json([
          {
            username: 'testuser1',
            message: 'Hello world!',
            abs_timestamp: '2025-10-19T10:05:00Z',
            rel_timestamp: capturedFrom + 300,
            badges: null,
            emotes: null,
            color: '#FF0000',
          },
        ])
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Should have used default from=0
    expect(capturedFrom).toBe(0)
  })

  it('handles messages without badges', async () => {
    server.use(
      http.get('/vods/1/chat', () => {
        return HttpResponse.json([
          {
            username: 'testuser1',
            message: 'Hello world!',
            abs_timestamp: '2025-10-19T10:05:00Z',
            rel_timestamp: 300,
            badges: null,
            emotes: null,
            color: null,
          },
        ])
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    // Should render without errors
    expect(screen.getByText('Hello world!')).toBeInTheDocument()
  })

  it('handles messages without color', async () => {
    server.use(
      http.get('/vods/1/chat', () => {
        return HttpResponse.json([
          {
            username: 'testuser1',
            message: 'Hello world!',
            abs_timestamp: '2025-10-19T10:05:00Z',
            rel_timestamp: 300,
            badges: null,
            emotes: null,
            color: null,
          },
        ])
      })
    )

    render(<ChatReplay vodId="1" />)

    await waitFor(() => {
      expect(screen.getByText('testuser1:')).toBeInTheDocument()
    })

    const username = screen.getByText('testuser1:')
    // Color should not be set (default styling)
    expect(username).not.toHaveStyle({ color: '#FF0000' })
  })
})
