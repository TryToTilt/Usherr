// Usherr: a JSON call to Usherr for the Marquee's pages, with the server's
// own words when it says no.
import { t } from '../i18n'

export async function marqueeCall<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    cache: 'no-store',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: { error?: string } | null = null
  try {
    data = text ? (JSON.parse(text) as { error?: string }) : null
  } catch {
    data = null
  }
  if (!res.ok) throw new Error(data?.error ?? t('Something went wrong (error {status}).', { status: res.status }))
  return data as T
}
