import '@testing-library/jest-dom'
import { afterAll, afterEach, beforeAll } from 'vitest'
import { setupServer } from 'msw/node'
import { http, HttpResponse } from 'msw'

// Mock API handlers
export const handlers = [
  // GET /vods - list all VODs (with pagination support)
  http.get('/vods', ({ request }) => {
    const url = new URL(request.url)
    const limit = parseInt(url.searchParams.get('limit') || '50', 10)
    const offset = parseInt(url.searchParams.get('offset') || '0', 10)

    // Sample data for testing
    const allVods = [
      {
        id: '1',
        title: 'Test VOD 1',
        date: '2025-10-19T10:00:00Z',
        processed: true,
        youtube_url: 'https://youtube.com/watch?v=test1',
      },
      {
        id: '2',
        title: 'Test VOD 2',
        date: '2025-10-19T11:00:00Z',
        processed: false,
        youtube_url: null,
      },
    ]

    // Apply pagination
    const paginatedVods = allVods.slice(offset, offset + limit)
    return HttpResponse.json(paginatedVods)
  }),

  // GET /vods/:id - get VOD details
  http.get('/vods/:id', ({ params }) => {
    const { id } = params
    if (id === '1') {
      return HttpResponse.json({
        id: '1',
        title: 'Test VOD 1',
        date: '2025-10-19T10:00:00Z',
        processed: true,
        youtube_url: 'https://youtube.com/watch?v=test1',
      })
    }
    if (id === '2') {
      return HttpResponse.json({
        id: '2',
        title: 'Test VOD 2',
        date: '2025-10-19T11:00:00Z',
        processed: false,
        youtube_url: null,
      })
    }
    return new HttpResponse(null, { status: 404 })
  }),

  // GET /vods/:id/progress - get VOD processing progress
  http.get('/vods/:id/progress', ({ params }) => {
    const { id } = params
    if (id === '1') {
      return HttpResponse.json({
        vod_id: '1',
        state: 'completed',
        percent: 100,
        retries: 0,
        total_bytes: 1024 * 1024 * 100,
        downloaded_path: '/data/vod-1.mp4',
        processed: true,
        youtube_url: 'https://youtube.com/watch?v=test1',
        progress_updated_at: '2025-10-19T10:30:00Z',
      })
    }
    if (id === '2') {
      return HttpResponse.json({
        vod_id: '2',
        state: 'downloading',
        percent: 45.5,
        retries: 1,
        total_bytes: 1024 * 1024 * 200,
        downloaded_path: null,
        processed: false,
        youtube_url: null,
        progress_updated_at: '2025-10-19T11:15:00Z',
      })
    }
    return new HttpResponse(null, { status: 404 })
  }),

  // GET /vods/:id/chat - get chat messages
  http.get('/vods/:id/chat', ({ request }) => {
    const url = new URL(request.url)
    const from = parseInt(url.searchParams.get('from') || '0', 10)

    return HttpResponse.json([
      {
        username: 'testuser1',
        message: 'Hello world!',
        abs_timestamp: '2025-10-19T10:05:00Z',
        rel_timestamp: from + 300,
        badges: 'broadcaster/1,subscriber/12',
        emotes: null,
        color: '#FF0000',
      },
      {
        username: 'testuser2',
        message: 'Great stream!',
        abs_timestamp: '2025-10-19T10:06:00Z',
        rel_timestamp: from + 360,
        badges: 'subscriber/6',
        emotes: null,
        color: '#00FF00',
      },
    ])
  }),
]

export const server = setupServer(...handlers)

// Start server before all tests
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))

// Reset handlers after each test
afterEach(() => server.resetHandlers())

// Clean up after all tests
afterAll(() => server.close())
