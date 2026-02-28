import { useEffect, useState } from 'react'
import { VodListSkeleton } from './Skeleton'

import { getApiBase } from '../lib/api'
const API_BASE_URL = getApiBase()

export interface Vod {
  id: string
  title: string
  date: string
  processed: boolean
  youtube_url?: string | null
}

interface VodListProps {
  onVodSelect?: (vodId: string) => void
}

export default function VodList({ onVodSelect }: VodListProps) {
  const [vods, setVods] = useState<Vod[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [retryCount, setRetryCount] = useState(0)
  const limit = 50 // Match backend default

  useEffect(() => {
    setLoading(true)
    setError(null)
    const offset = page * limit
    // Fetch limit + 1 to detect if there are more pages
    fetch(`${API_BASE_URL}/vods?limit=${limit + 1}&offset=${offset}`)
      .then((res) => {
        if (!res.ok) throw new Error('Failed to fetch VODs')
        return res.json()
      })
      .then((data: Vod[]) => {
        // If we got more than limit items, there's a next page
        setHasMore(data.length > limit)
        // Only show limit items
        setVods(data.slice(0, limit))
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [page, retryCount])

  if (loading) return <VodListSkeleton />
  
  if (error) {
    return (
      <div className="p-4" role="alert">
        <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded">
          <div className="flex items-start">
            <div className="flex-shrink-0">
              <svg
                className="h-5 w-5 text-red-400"
                viewBox="0 0 20 20"
                fill="currentColor"
                aria-hidden="true"
              >
                <path
                  fillRule="evenodd"
                  d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
                  clipRule="evenodd"
                />
              </svg>
            </div>
            <div className="ml-3 flex-1">
              <h3 className="text-sm font-medium text-red-800">
                Failed to load VODs
              </h3>
              <div className="mt-2 text-sm text-red-700">
                <p>{error}</p>
              </div>
            </div>
          </div>
        </div>
        <button
          onClick={() => setRetryCount((prev) => prev + 1)}
          className="px-4 py-2 bg-indigo-600 text-white rounded hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
          aria-label="Retry loading VODs"
        >
          Retry
        </button>
      </div>
    )
  }

  if (vods.length === 0) {
    return (
      <div className="p-4">
        <h2 className="text-xl font-bold mb-4">Twitch VODs</h2>
        <div className="text-center py-12 bg-gray-50 rounded border border-gray-200">
          <svg
            className="mx-auto h-12 w-12 text-gray-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            aria-hidden="true"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z"
            />
          </svg>
          <h3 className="mt-2 text-sm font-medium text-gray-900">No VODs found</h3>
          <p className="mt-1 text-sm text-gray-500">
            No VODs have been archived yet. Check back later!
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="p-4">
      <h2 className="text-xl font-bold mb-4">Twitch VODs</h2>
      <div className="overflow-x-auto">
        <table
          className="min-w-full border border-gray-200 bg-white shadow-sm"
          role="table"
          aria-label="List of Twitch VODs"
        >
          <thead>
            <tr className="bg-gray-100">
              <th className="px-4 py-2 text-left" scope="col">
                Title
              </th>
              <th className="px-4 py-2 text-left" scope="col">
                Date
              </th>
              <th className="px-4 py-2 text-left" scope="col">
                Status
              </th>
              <th className="px-4 py-2 text-left" scope="col">
                YouTube
              </th>
            </tr>
          </thead>
          <tbody>
            {vods.map((vod) => (
              <tr
                key={vod.id}
                className={
                  'border-t hover:bg-indigo-50 cursor-pointer' +
                  (onVodSelect ? ' transition' : '')
                }
                onClick={onVodSelect ? () => onVodSelect(vod.id) : undefined}
                onKeyDown={
                  onVodSelect
                    ? (e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          onVodSelect(vod.id)
                        }
                      }
                    : undefined
                }
                tabIndex={onVodSelect ? 0 : undefined}
                aria-label={`View details for ${vod.title}`}
              >
                <td className="px-4 py-2 font-medium">{vod.title}</td>
                <td className="px-4 py-2">
                  <time dateTime={vod.date}>
                    {new Date(vod.date).toLocaleString()}
                  </time>
                </td>
                <td className="px-4 py-2">
                  {vod.processed ? (
                    <span className="text-green-600">
                      Processed
                    </span>
                  ) : (
                    <span className="text-yellow-700">
                      Pending
                    </span>
                  )}
                </td>
                <td className="px-4 py-2">
                  {vod.youtube_url ? (
                    <a
                      href={vod.youtube_url}
                      className="text-blue-600 underline hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => e.stopPropagation()}
                      aria-label={`Watch ${vod.title} on YouTube`}
                    >
                      YouTube
                    </a>
                  ) : (
                    <span className="text-gray-400">
                      —
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <nav
        className="mt-4 flex justify-between items-center"
        aria-label="Pagination"
        role="navigation"
      >
        <button
          onClick={() => setPage((p) => Math.max(0, p - 1))}
          disabled={page === 0 || loading}
          className="px-4 py-2 bg-indigo-600 text-white rounded disabled:bg-gray-400 disabled:cursor-not-allowed hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
          aria-label="Go to previous page"
        >
          Previous
        </button>
        <span className="text-gray-600" aria-live="polite" aria-atomic="true">
          Page {page + 1}
        </span>
        <button
          onClick={() => setPage((p) => p + 1)}
          disabled={!hasMore || loading}
          className="px-4 py-2 bg-indigo-600 text-white rounded disabled:bg-gray-400 disabled:cursor-not-allowed hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
          aria-label="Go to next page"
        >
          Next
        </button>
      </nav>
    </div>
  )
}
