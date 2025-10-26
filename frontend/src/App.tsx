import { useState } from 'react'
import VodDetail from './components/VodDetail'
import VodList from './components/VodList'

function App() {
  const [selectedVod, setSelectedVod] = useState<string | null>(null)

  return (
    <div className="flex flex-col min-h-screen bg-green-500">
      <header className="px-6 py-4 text-white bg-indigo-700 shadow">
        <h1 className="text-2xl font-bold tracking-tight">
          VOD Tender Dashboardddddddd
        </h1>
      </header>
      <main className="flex flex-col items-center justify-start flex-1">
        <div className="w-full max-w-4xl mt-8">
          {selectedVod ? (
            <VodDetail
              vodId={selectedVod}
              onBack={() => setSelectedVod(null)}
            />
          ) : (
            <VodList onVodSelect={setSelectedVod} />
          )}
        </div>
      </main>
      <footer className="py-4 text-xs text-center text-gray-400">
        &copy; {new Date().getFullYear()} VOD Tender
      </footer>
    </div>
  )
}

export default App
