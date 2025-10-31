import { useEffect, useState } from 'react'

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
  const limit = 50 // Match backend default

  useEffect(() => {
    setLoading(true)
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
  }, [page])

  if (loading) return <div className="p-4">Loading VODs...</div>
  if (error) return <div className="p-4 text-red-500">{error}</div>

  return (
    <div className="p-4">
      <h2 className="text-xl font-bold mb-4">Twitch VODs</h2>
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
      <div className="mt-4 flex justify-between items-center">
        <button
          onClick={() => setPage((p) => Math.max(0, p - 1))}
          disabled={page === 0 || loading}
          className="px-4 py-2 bg-indigo-600 text-white rounded disabled:bg-gray-400 disabled:cursor-not-allowed"
        >
          Previous
        </button>
        <span className="text-gray-600">Page {page + 1}</span>
        <button
          onClick={() => setPage((p) => p + 1)}
          disabled={!hasMore || loading}
          className="px-4 py-2 bg-indigo-600 text-white rounded disabled:bg-gray-400 disabled:cursor-not-allowed"
        >
          Next
        </button>
      </div>
    </div>
  )
}
