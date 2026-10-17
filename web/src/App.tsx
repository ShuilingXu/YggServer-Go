import { useEffect } from 'react'
import { getHealth } from './api'

export function App() {
  useEffect(() => {
    const controller = new AbortController()
    getHealth(controller.signal).catch(() => {
      if (!controller.signal.aborted) console.error('API health check failed')
    })
    return () => controller.abort()
  }, [])
  return null
}
