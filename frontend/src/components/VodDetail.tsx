import { useEffect, useState } from 'react'
import ChatReplay from './ChatReplay'
import { VodDetailSkeleton } from './Skeleton'
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
  processing_error?: string
  youtube_url?: string | null
  progress_updated_at?: string
}

export default function VodDetail({ vodId, onBack }: VodDetailProps) {
  const [vod, setVod] = useState<Vod | null>(null)
  const [progress, setProgress] = useState<Progress | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [retryCount, setRetryCount] = useState(0)

  useEffect(() => {
    let cancelled = false
    let timeoutId: number | null = null

    const fetchData = async () => {
      if (cancelled) return

      try {
        setLoading(true)
        setError(null)
        const [vodData, progressData] = await Promise.all([
          fetch(`${API_BASE_URL}/vods/${vodId}`).then((r) => r.json()),
          fetch(`${API_BASE_URL}/vods/${vodId}/progress`).then((r) => r.json()),
        ])

        if (cancelled) return

        setVod(vodData)
        setProgress(progressData)
        setError(null)

        // Poll if VOD is not processed and has no error
        const shouldPoll =
          !progressData.processed && !progressData.processing_error
        if (shouldPoll) {
          timeoutId = window.setTimeout(fetchData, 3000) // Poll every 3 seconds
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : 'Unknown error')
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    fetchData()

    return () => {
      cancelled = true
      if (timeoutId !== null) {
        clearTimeout(timeoutId)
      }
    }
  }, [vodId, retryCount])

  if (loading) return <VodDetailSkeleton />
  
  if (error) {
    return (
      <div className="p-4" role="alert">
        <button
          onClick={onBack}
          className="mb-4 text-blue-600 underline hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
          aria-label="Go back to VOD list"
        >
          &larr; Back to list
        </button>
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
                Failed to load VOD details
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
          aria-label="Retry loading VOD details"
        >
          Retry
        </button>
      </div>
    )
  }
  
  if (!vod) {
    return (
      <div className="p-4">
        <button
          onClick={onBack}
          className="mb-4 text-blue-600 underline hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
          aria-label="Go back to VOD list"
        >
          &larr; Back to list
        </button>
        <div className="text-center py-12 bg-gray-50 rounded border border-gray-200">
          <h3 className="text-sm font-medium text-gray-900">VOD not found</h3>
          <p className="mt-1 text-sm text-gray-500">
            The requested VOD could not be found.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="p-4">
      <button
        onClick={onBack}
        className="mb-4 text-blue-600 underline hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
        aria-label="Go back to VOD list"
      >
        &larr; Back to list
      </button>
      <h2 className="text-2xl font-bold mb-2">{vod.title}</h2>
      <div className="text-gray-600 mb-2">
        <time dateTime={vod.date}>{new Date(vod.date).toLocaleString()}</time>
      </div>
      <div className="mb-4">
        {vod.processed ? (
          <span className="text-green-600">
            Processed
          </span>
        ) : (
          <span className="text-yellow-700">
            Pending
          </span>
        )}
      </div>
      {progress && (
        <div className="mb-4">
          <div className="mb-1 text-sm text-gray-500">Progress</div>
          <div
            className="w-full bg-gray-200 rounded h-4 overflow-hidden"
            role="progressbar"
            aria-valuenow={progress.percent ?? 0}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-label={`Download progress: ${progress.percent?.toFixed(1) ?? 0}%`}
          >
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
      {progress?.processing_error && (
        <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded" role="alert">
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
                Processing Error
              </h3>
              <div className="mt-2 text-sm text-red-700">
                <p>{progress.processing_error}</p>
              </div>
              <div className="mt-3 text-sm">
                <p className="text-red-600">
                  <strong>Retry Guidance:</strong> The system will automatically
                  retry this VOD after a cooldown period. If the issue persists,
                  you can use the reprocess action from the VOD list to manually
                  retry.
                </p>
              </div>
            </div>
          </div>
        </div>
      )}
      {vod.youtube_url && (
        <div className="mb-4">
          <a
            href={vod.youtube_url}
            className="text-blue-600 underline hover:text-blue-800 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2"
            target="_blank"
            rel="noopener noreferrer"
            aria-label={`Watch ${vod.title} on YouTube`}
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
