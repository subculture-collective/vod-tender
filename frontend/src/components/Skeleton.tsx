interface SkeletonProps {
  className?: string
}

export default function Skeleton({ className = '' }: SkeletonProps) {
  return (
    <div
      className={`animate-pulse bg-gray-300 rounded ${className}`}
      aria-hidden="true"
    />
  )
}

export function VodListSkeleton() {
  return (
    <div className="p-4" role="status" aria-label="Loading VODs">
      <Skeleton className="h-8 w-48 mb-4" />
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
            {Array.from({ length: 5 }).map((_, i) => (
              <tr key={i} className="border-t">
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-64" />
                </td>
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-40" />
                </td>
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-20" />
                </td>
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-16" />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-4 flex justify-between items-center">
        <Skeleton className="h-10 w-24" />
        <Skeleton className="h-5 w-16" />
        <Skeleton className="h-10 w-24" />
      </div>
      <span className="sr-only">Loading VODs...</span>
    </div>
  )
}

export function VodDetailSkeleton() {
  return (
    <div className="p-4" role="status" aria-label="Loading VOD details">
      <Skeleton className="h-6 w-32 mb-4" />
      <Skeleton className="h-8 w-96 mb-2" />
      <Skeleton className="h-5 w-48 mb-2" />
      <Skeleton className="h-5 w-24 mb-4" />
      <div className="mb-4">
        <Skeleton className="h-4 w-16 mb-1" />
        <Skeleton className="h-4 w-full mb-1" />
        <Skeleton className="h-4 w-48" />
      </div>
      <div className="bg-gray-100 rounded p-2 mt-6">
        <Skeleton className="h-6 w-32 mb-2" />
        <Skeleton className="h-64 w-full" />
      </div>
      <span className="sr-only">Loading VOD details...</span>
    </div>
  )
}
