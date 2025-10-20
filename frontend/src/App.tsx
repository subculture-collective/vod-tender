import { useState } from 'react'
import VodDetail from './components/VodDetail'
import VodList from './components/VodList'

function App() {
  const [selectedVod, setSelectedVod] = useState<string | null>(null)

  return (
    <div className="min-h-screen bg-gray-50 flex flex-col">
      <header className="bg-indigo-700 text-white py-4 px-6 shadow">
        <h1 className="text-2xl font-bold tracking-tight">
          VOD Tender Dashboard
        </h1>
      </header>
      <main className="flex-1 flex flex-col items-center justify-start">
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
      <footer className="text-center text-xs text-gray-400 py-4">
        &copy; {new Date().getFullYear()} VOD Tender
      </footer>
    </div>
  )
}

export default App
