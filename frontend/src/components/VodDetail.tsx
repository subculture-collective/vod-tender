import { useEffect, useState } from 'react'
import ChatReplay from './ChatReplay'
import type { Vod } from './VodList'

import { getApiBase } from '../lib/api'
const API_BASE_URL = getApiBase()

interface VodDetailProps {
  vodId: string
  onBack: () => void
}

interface Progress {
  vod_id: string
  state?: string
  percent?: number
  retries: number
  total_bytes?: number
  downloaded_path?: string
  processed: boolean
  youtube_url?: string | null
  progress_updated_at?: string
}

export default function VodDetail({ vodId, onBack }: VodDetailProps) {
  const [vod, setVod] = useState<Vod | null>(null)
  const [progress, setProgress] = useState<Progress | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    Promise.all([
      fetch(`${API_BASE_URL}/vods/${vodId}`).then((r) => r.json()),
      fetch(`${API_BASE_URL}/vods/${vodId}/progress`).then((r) => r.json()),
    ])
      .then(([vodData, progressData]) => {
        setVod(vodData)
        setProgress(progressData)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [vodId])

  if (loading) return <div className="p-4">Loading VOD...</div>
  if (error) return <div className="p-4 text-red-500">{error}</div>
  if (!vod) return <div className="p-4">VOD not found.</div>

  return (
    <div className="p-4">
      <button onClick={onBack} className="mb-4 text-blue-600 underline">
        &larr; Back to list
      </button>
      <h2 className="text-2xl font-bold mb-2">{vod.title}</h2>
      <div className="text-gray-600 mb-2">
        {new Date(vod.date).toLocaleString()}
      </div>
      <div className="mb-4">
        {vod.processed ? (
          <span className="text-green-600">Processed</span>
        ) : (
          <span className="text-yellow-600">Pending</span>
        )}
      </div>
      {progress && (
        <div className="mb-4">
          <div className="mb-1 text-sm text-gray-500">Progress</div>
          <div className="w-full bg-gray-200 rounded h-4 overflow-hidden">
            <div
              className="bg-indigo-500 h-4"
              style={{ width: `${progress.percent ?? 0}%` }}
            ></div>
          </div>
          <div className="text-xs text-gray-600 mt-1">
            {progress.state || '-'} ({progress.percent?.toFixed(1) ?? 0}%)
            {progress.total_bytes
              ? `, ${Math.round(progress.total_bytes / 1024 / 1024)} MB`
              : ''}
            {progress.retries > 0 ? `, retries: ${progress.retries}` : ''}
          </div>
        </div>
      )}
      {vod.youtube_url && (
        <div className="mb-4">
          <a
            href={vod.youtube_url}
            className="text-blue-600 underline"
            target="_blank"
            rel="noopener noreferrer"
          >
            Watch on YouTube
          </a>
        </div>
      )}
      {/* Chat and actions can be added here */}
      <div>
        <ChatReplay vodId={vodId} />
      </div>
    </div>
  )
}
