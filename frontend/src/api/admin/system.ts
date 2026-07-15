/**
 * System API endpoints for admin operations
 */

import { apiClient } from '../client'

const CUSTOM_UPDATE_REQUEST_TIMEOUT_MS = 11 * 60 * 1000
const PUBLIC_VERSION_REQUEST_TIMEOUT_MS = 5 * 1000

export interface ReleaseInfo {
  name: string
  body: string
  published_at: string
  html_url: string
}

export interface VersionInfo {
  current_version: string
  latest_version: string
  has_update: boolean
  release_info?: ReleaseInfo
  cached: boolean
  warning?: string
  build_type: string // "source", "release", or "custom"
  custom_version?: string
  custom_image?: string
  custom_release_url?: string
  custom_update_available?: boolean
  custom_update_ready?: boolean
  custom_update_warning?: string
}

/**
 * Get current version
 */
export async function getVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/admin/system/version')
  return data
}

/**
 * Read the running version from the unauthenticated public settings endpoint.
 */
export async function getPublicVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/settings/public', {
    timeout: PUBLIC_VERSION_REQUEST_TIMEOUT_MS
  })
  return { version: data.version }
}

/**
 * Check for updates
 * @param force - Force refresh from GitHub API
 */
export async function checkUpdates(force = false): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/check-updates', {
    params: force ? { force: 'true' } : undefined
  })
  return data
}

export interface UpdateResult {
  message: string
  need_restart: boolean
  automatic_restart?: boolean
  target_version?: string
  target_image?: string
}

export interface RollbackVersionInfo {
  version: string
  published_at: string
  html_url: string
}

/**
 * Get versions available for rollback (up to 3 versions older than current)
 */
export async function getRollbackVersions(): Promise<{ versions: RollbackVersionInfo[] }> {
  const { data } = await apiClient.get<{ versions: RollbackVersionInfo[] }>(
    '/admin/system/rollback-versions'
  )
  return data
}

/**
 * Perform system update
 * Downloads and applies the latest version
 */
export async function performUpdate(): Promise<UpdateResult> {
  const { data } = await apiClient.post<UpdateResult>('/admin/system/update', undefined, {
    timeout: CUSTOM_UPDATE_REQUEST_TIMEOUT_MS
  })
  return data
}

/**
 * Rollback to a previous version
 * @param version - Target version (e.g. "0.1.146"); omit to restore the local backup binary
 */
export async function rollback(version?: string): Promise<UpdateResult> {
  const { data } = await apiClient.post<UpdateResult>(
    '/admin/system/rollback',
    version ? { version } : undefined
  )
  return data
}

/**
 * Restart the service
 */
export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart')
  return data
}

export const systemAPI = {
  getVersion,
  getPublicVersion,
  checkUpdates,
  performUpdate,
  getRollbackVersions,
  rollback,
  restartService
}

export default systemAPI
