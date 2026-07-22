import { apiClient } from '../client'

export const CUSTOM_IMAGE_UPDATE_REQUEST_TIMEOUT_MS = 11 * 60 * 1000
export const CUSTOM_IMAGE_PUBLIC_VERSION_TIMEOUT_MS = 5 * 1000
export const CUSTOM_IMAGE_POLL_INITIAL_DELAY_MS = 1_500
export const CUSTOM_IMAGE_POLL_INTERVAL_MS = 2_000
export const CUSTOM_IMAGE_POLL_TIMEOUT_MS = 10 * 60 * 1000

export interface CustomImageUpdateInfo {
  current_version: string
  target_version?: string
  image?: string
  target_image?: string
  target_digest?: string
  release_url?: string
  has_update: boolean
  target_ready: boolean
  latest_alias_ready: boolean
  ready: boolean
  warning?: string
}

export interface CustomImageUpdateResult {
  message: string
  operation_id: string
  target_version: string
  target_image: string
  target_digest: string
  automatic_restart: boolean
}

export interface WaitForCustomImageVersionOptions {
  signal?: AbortSignal
  initialDelayMs?: number
  pollIntervalMs?: number
  timeoutMs?: number
}

export class CustomImageRestartTimeoutError extends Error {
  readonly code = 'CUSTOM_IMAGE_RESTART_TIMEOUT'

  constructor(targetVersion: string) {
    super(`The new custom image ${targetVersion} was not detected before the restart deadline.`)
    this.name = 'CustomImageRestartTimeoutError'
  }
}

export async function checkCustomImageUpdate(): Promise<CustomImageUpdateInfo> {
  const { data } = await apiClient.get<CustomImageUpdateInfo>('/admin/system/custom-image/check')
  return data
}

export async function triggerCustomImageUpdate(): Promise<CustomImageUpdateResult> {
  const { data } = await apiClient.post<CustomImageUpdateResult>(
    '/admin/system/custom-image/update',
    undefined,
    { timeout: CUSTOM_IMAGE_UPDATE_REQUEST_TIMEOUT_MS }
  )
  return data
}

export async function getPublicVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/settings/public', {
    timeout: CUSTOM_IMAGE_PUBLIC_VERSION_TIMEOUT_MS
  })
  return { version: data.version }
}

export async function waitForCustomImageVersion(
  targetVersion: string,
  options: WaitForCustomImageVersionOptions = {}
): Promise<{ version: string } | null> {
  const signal = options.signal
  const initialDelayMs = options.initialDelayMs ?? CUSTOM_IMAGE_POLL_INITIAL_DELAY_MS
  const pollIntervalMs = options.pollIntervalMs ?? CUSTOM_IMAGE_POLL_INTERVAL_MS
  const timeoutMs = options.timeoutMs ?? CUSTOM_IMAGE_POLL_TIMEOUT_MS
  const deadline = Date.now() + timeoutMs

  if (!(await delay(initialDelayMs, signal))) {
    return null
  }

  while (!signal?.aborted && Date.now() < deadline) {
    try {
      const info = await getPublicVersion()
      if (info.version === targetVersion) {
        return info
      }
    } catch {
      // Temporary connection failures are expected while Watchtower replaces the container.
    }

    const remaining = deadline - Date.now()
    if (remaining <= 0) {
      break
    }
    if (!(await delay(Math.min(pollIntervalMs, remaining), signal))) {
      return null
    }
  }

  if (signal?.aborted) {
    return null
  }
  throw new CustomImageRestartTimeoutError(targetVersion)
}

export function isExpectedContainerReplacementError(error: unknown): boolean {
  if (!error || typeof error !== 'object') {
    return false
  }
  const candidate = error as {
    status?: number
    response?: unknown
    message?: string
  }
  if (candidate.response) {
    return false
  }
  if (candidate.status !== undefined) {
    return candidate.status === 0
  }
  return typeof candidate.message === 'string' && candidate.message.length > 0
}

function delay(ms: number, signal?: AbortSignal): Promise<boolean> {
  if (signal?.aborted) {
    return Promise.resolve(false)
  }
  return new Promise((resolve) => {
    const timeout = window.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve(true)
    }, Math.max(0, ms))
    const onAbort = () => {
      window.clearTimeout(timeout)
      signal?.removeEventListener('abort', onAbort)
      resolve(false)
    }
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

export default {
  check: checkCustomImageUpdate,
  trigger: triggerCustomImageUpdate,
  getPublicVersion,
  waitForVersion: waitForCustomImageVersion
}
