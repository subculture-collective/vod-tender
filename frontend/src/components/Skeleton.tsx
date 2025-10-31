interface SkeletonProps {
  className?: string
}

export function Skeleton({ className = '' }: SkeletonProps) {
  return (
    <div
      className={`animate-pulse bg-gray-300 rounded ${className}`}
      aria-busy="true"
      aria-live="polite"
    />
  )
}

export function VodTableSkeleton() {
  return (
    <div className="p-4">
      <div className="mb-4">
        <Skeleton className="h-7 w-32" />
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
            {Array.from({ length: 5 }).map((_, i) => (
              <tr key={i} className="border-t">
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-48" />
                </td>
                <td className="px-4 py-2">
                  <Skeleton className="h-5 w-32" />
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
    </div>
  )
}
