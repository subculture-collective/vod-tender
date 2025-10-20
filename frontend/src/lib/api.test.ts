import { describe, it, expect, beforeEach, vi } from 'vitest'
import { getApiBase } from './api'

describe('getApiBase', () => {
  beforeEach(() => {
    // Clear any environment variables
    vi.stubEnv('VITE_API_BASE_URL', '')
  })

  it('returns VITE_API_BASE_URL when provided', () => {
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com')
    const result = getApiBase()
    expect(result).toBe('https://api.example.com')
  })

  it('maps vod-tender domain to vod-api domain', () => {
    vi.stubEnv('VITE_API_BASE_URL', '')
    
    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: {
        href: 'https://vod-tender.example.com/dashboard',
        protocol: 'https:',
        host: 'vod-tender.example.com',
      },
      writable: true,
    })

    const result = getApiBase()
    expect(result).toBe('https://vod-api.example.com')
  })

  it('returns same-origin URL when not vod-tender domain', () => {
    vi.stubEnv('VITE_API_BASE_URL', '')
    
    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: {
        href: 'https://example.com/dashboard',
        protocol: 'https:',
        host: 'example.com',
      },
      writable: true,
    })

    const result = getApiBase()
    expect(result).toBe('https://example.com')
  })

  it('handles http protocol', () => {
    vi.stubEnv('VITE_API_BASE_URL', '')
    
    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: {
        href: 'http://localhost:3000',
        protocol: 'http:',
        host: 'localhost:3000',
      },
      writable: true,
    })

    const result = getApiBase()
    expect(result).toBe('http://localhost:3000')
  })

  it('trims whitespace from VITE_API_BASE_URL', () => {
    vi.stubEnv('VITE_API_BASE_URL', '  https://api.example.com  ')
    const result = getApiBase()
    expect(result).toBe('https://api.example.com')
  })

  it('ignores empty VITE_API_BASE_URL and uses domain mapping', () => {
    vi.stubEnv('VITE_API_BASE_URL', '   ')
    
    // Mock window.location
    Object.defineProperty(window, 'location', {
      value: {
        href: 'https://vod-tender.test.com',
        protocol: 'https:',
        host: 'vod-tender.test.com',
      },
      writable: true,
    })

    const result = getApiBase()
    expect(result).toBe('https://vod-api.test.com')
  })
})
