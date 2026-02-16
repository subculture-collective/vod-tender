import { useState } from 'react'
import VodDetail from './components/VodDetail'
import VodList from './components/VodList'

function App() {
  const [selectedVod, setSelectedVod] = useState<string | null>(null)

  return (
    <div className="flex flex-col min-h-screen bg-gray-50">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:px-4 focus:py-2 focus:bg-indigo-600 focus:text-white focus:rounded focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
      >
        Skip to main content
      </a>
      <header className="px-6 py-4 text-white bg-indigo-700 shadow" role="banner">
        <h1 className="text-2xl font-bold tracking-tight">
          VOD Tender Dashboard
        </h1>
      </header>
      <main
        id="main-content"
        className="flex flex-col items-center justify-start flex-1"
        role="main"
      >
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
      <footer className="py-4 text-xs text-center text-gray-400" role="contentinfo">
        &copy; {new Date().getFullYear()} VOD Tender
      </footer>
    </div>
  )
}

export default App
