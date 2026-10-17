export async function getHealth(signal: AbortSignal): Promise<void> {
  const response = await fetch('/healthz', { signal, credentials: 'same-origin' })
  if (!response.ok) throw new Error('API health check failed')
  const body: unknown = await response.json()
  if (typeof body !== 'object' || body === null || !('status' in body) || body.status !== 'ok') {
    throw new Error('Invalid API health response')
  }
}
