class InMemoryStorage implements Storage {
  private readonly store = new Map<string, string>()

  get length(): number {
    return this.store.size
  }

  clear(): void {
    this.store.clear()
  }

  getItem(key: string): string | null {
    return this.store.get(key) ?? null
  }

  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null
  }

  removeItem(key: string): void {
    this.store.delete(key)
  }

  setItem(key: string, value: string): void {
    this.store.set(String(key), String(value))
  }
}

function hasStorageAPI(value: unknown): value is Storage {
  if (!value || typeof value !== 'object') {
    return false
  }

  const candidate = value as Partial<Storage>
  return (
    typeof candidate.getItem === 'function' &&
    typeof candidate.setItem === 'function' &&
    typeof candidate.removeItem === 'function' &&
    typeof candidate.clear === 'function' &&
    typeof candidate.key === 'function'
  )
}

if (!hasStorageAPI((globalThis as { localStorage?: unknown }).localStorage)) {
  const storage = new InMemoryStorage()

  try {
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      writable: true,
      value: storage,
    })
  } catch {
    ;(globalThis as { localStorage: Storage }).localStorage = storage
  }
}
