import { useEffect, useState } from 'react'

import { getApiBase } from '../lib/api'
import { VodTableSkeleton } from './Skeleton'

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

const ITEMS_PER_PAGE = 50

export default function VodList({ onVodSelect }: VodListProps) {
  const [vods, setVods] = useState<Vod[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [currentPage, setCurrentPage] = useState(1)
  const [hasMore, setHasMore] = useState(true)

  useEffect(() => {
    setLoading(true)
    const offset = (currentPage - 1) * ITEMS_PER_PAGE
    fetch(`${API_BASE_URL}/vods?limit=${ITEMS_PER_PAGE}&offset=${offset}`)
      .then((res) => {
        if (!res.ok) throw new Error('Failed to fetch VODs')
        return res.json()
      })
      .then((data) => {
        setVods(data)
        // If we received fewer items than requested, we're on the last page
        setHasMore(data.length === ITEMS_PER_PAGE)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [currentPage])

  if (loading) return <VodTableSkeleton />
  if (error) return <div className="p-4 text-red-500">{error}</div>

  const handlePrevPage = () => {
    if (currentPage > 1) {
      setCurrentPage(currentPage - 1)
    }
  }

  const handleNextPage = () => {
    if (hasMore) {
      setCurrentPage(currentPage + 1)
    }
  }

  return (
    <div className="p-4">
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-bold">Twitch VODs</h2>
        <div className="text-sm text-gray-600">
          Page {currentPage} • Showing {vods.length} VODs
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full border border-gray-200 bg-white shadow-sm">
          <thead>
            <tr className="bg-gray-100">
              <th className="px-4 py-2 text-left">Title</th>
              <th className="px-4 py-2 text-left">Date</th>
              <th className="px-4 py-2 text-left">Status</th>
              <th className="px-4 py-2 text-left">YouTube</th>
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
              >
                <td className="px-4 py-2 font-medium">{vod.title}</td>
                <td className="px-4 py-2">
                  {new Date(vod.date).toLocaleString()}
                </td>
                <td className="px-4 py-2">
                  {vod.processed ? (
                    <span className="text-green-600">Processed</span>
                  ) : (
                    <span className="text-yellow-600">Pending</span>
                  )}
                </td>
                <td className="px-4 py-2">
                  {vod.youtube_url ? (
                    <a
                      href={vod.youtube_url}
                      className="text-blue-600 underline"
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => e.stopPropagation()}
                    >
                      YouTube
                    </a>
                  ) : (
                    <span className="text-gray-400">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {vods.length === 0 ? (
        <div className="mt-4 p-4 text-center text-gray-500">
          No VODs found
        </div>
      ) : (
        <div className="mt-4 flex items-center justify-between">
          <button
            onClick={handlePrevPage}
            disabled={currentPage === 1}
            className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-md hover:bg-indigo-700 disabled:bg-gray-300 disabled:cursor-not-allowed"
            aria-label="Previous page"
          >
            Previous
          </button>
          <span className="text-sm text-gray-600">Page {currentPage}</span>
          <button
            onClick={handleNextPage}
            disabled={!hasMore}
            className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-md hover:bg-indigo-700 disabled:bg-gray-300 disabled:cursor-not-allowed"
            aria-label="Next page"
          >
            Next
          </button>
        </div>
      )}
    </div>
  )
}
