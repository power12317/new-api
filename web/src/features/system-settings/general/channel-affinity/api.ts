/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

import type { CacheEntryList, CacheStats } from './types'

export async function getCacheStats(): Promise<{
  success: boolean
  message?: string
  data?: CacheStats
}> {
  const res = await api.get('/api/option/channel_affinity_cache', {
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}

export async function clearAllCache(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.delete('/api/option/channel_affinity_cache', {
    params: { all: true },
  })
  return res.data
}

export async function clearRuleCache(
  ruleName: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete('/api/option/channel_affinity_cache', {
    params: { rule_name: ruleName },
  })
  return res.data
}

export async function getCacheEntries(
  params: {
    page?: number
    page_size?: number
    query?: string
    rule_name?: string
    channel_id?: number
  } = {}
): Promise<{
  success: boolean
  message?: string
  data?: CacheEntryList
}> {
  const res = await api.get('/api/option/channel_affinity_cache/entries', {
    params,
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}

export async function updateCacheEntry(params: {
  key: string
  channel_id: number
  ttl_seconds?: number
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/option/channel_affinity_cache/entry', params)
  return res.data
}

export async function deleteCacheEntry(
  key: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete('/api/option/channel_affinity_cache/entry', {
    params: { key },
  })
  return res.data
}

export async function getAffinityUsageCache(params: {
  rule_name: string
  using_group: string
  key_hint: string
  key_fp: string
}): Promise<{ success: boolean; message?: string; data?: unknown }> {
  const res = await api.get('/api/log/channel_affinity_usage_cache', {
    params,
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}
