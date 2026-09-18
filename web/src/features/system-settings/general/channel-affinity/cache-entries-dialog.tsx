import { Edit, RefreshCw, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'

import { deleteCacheEntry, getCacheEntries, updateCacheEntry } from './api'
import type { CacheEntry } from './types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function CacheEntriesDialog(props: Props) {
  const { t } = useTranslation()
  const [entries, setEntries] = useState<CacheEntry[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [query, setQuery] = useState('')
  const [editing, setEditing] = useState<CacheEntry | null>(null)
  const [channelID, setChannelID] = useState('')
  const [ttlSeconds, setTtlSeconds] = useState('')
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState<CacheEntry | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getCacheEntries({
        page: 1,
        page_size: 500,
        query: query.trim() || undefined,
      })
      if (!response.success) {
        handleServerError(response, t('Request failed'))
        return
      }
      setEntries(response.data?.entries || [])
      setTotal(response.data?.total || 0)
    } catch (error) {
      handleServerError(error, t('Failed to load channel affinity cache'))
    } finally {
      setLoading(false)
    }
  }, [query, t])

  useEffect(() => {
    if (props.open) void refresh()
  }, [props.open, refresh])

  const openEdit = (entry: CacheEntry) => {
    setEditing(entry)
    setChannelID(String(entry.channel_id))
    setTtlSeconds(entry.ttl_seconds > 0 ? String(entry.ttl_seconds) : '')
  }

  const saveEdit = async () => {
    if (!editing) return
    const nextChannelID = Number(channelID)
    const nextTTL = Number(ttlSeconds || 0)
    if (!Number.isInteger(nextChannelID) || nextChannelID <= 0) {
      toast.error(t('Channel ID must be a positive integer'))
      return
    }
    setSaving(true)
    try {
      const response = await updateCacheEntry({
        key: editing.key,
        channel_id: nextChannelID,
        ttl_seconds: nextTTL,
      })
      if (!response.success) {
        handleServerError(response, t('Request failed'))
        return
      }
      toast.success(t('Affinity cache entry updated'))
      setEditing(null)
      await refresh()
    } catch (error) {
      handleServerError(error, t('Failed to update affinity cache entry'))
    } finally {
      setSaving(false)
    }
  }

  const removeEntry = async (entry: CacheEntry) => {
    setDeleting(null)
    try {
      const response = await deleteCacheEntry(entry.key)
      if (!response.success) {
        handleServerError(response, t('Request failed'))
        return
      }
      toast.success(t('Affinity cache entry deleted'))
      await refresh()
    } catch (error) {
      handleServerError(error, t('Failed to delete affinity cache entry'))
    }
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={props.onOpenChange}
        title={t('Channel Affinity Cache Entries')}
        contentClassName='max-w-6xl'
        contentHeight='full'
        bodyClassName='space-y-4'
      >
        <div className='flex flex-wrap items-end gap-2'>
          <div className='grid min-w-[240px] flex-1 gap-1.5'>
            <Label htmlFor='channel-affinity-cache-query'>
              {t('Search cache entries')}
            </Label>
            <Input
              id='channel-affinity-cache-query'
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t('Search by key, user, or channel')}
            />
          </div>
          <Button
            variant='outline'
            onClick={() => void refresh()}
            disabled={loading}
          >
            <RefreshCw
              className={loading ? 'mr-1 h-4 w-4 animate-spin' : 'mr-1 h-4 w-4'}
            />
            {t('Refresh')}
          </Button>
          <span className='text-muted-foreground pb-2 text-xs'>
            {t('Entries')}: {total}
          </span>
        </div>

        <StaticDataTable
          tableClassName='min-w-max'
          data={entries}
          emptyContent={loading ? t('Loading...') : t('No data available')}
          columns={[
            {
              id: 'key',
              header: t('Cache Key'),
              cellClassName: 'max-w-[280px] font-mono text-xs',
              cell: (entry) => entry.key_suffix || entry.key,
            },
            {
              id: 'identity',
              header: t('Affinity Identity'),
              cell: (entry) => (
                <div className='space-y-0.5 text-xs'>
                  <div>
                    {entry.username ||
                      entry.token_name ||
                      entry.key_hint ||
                      '-'}
                  </div>
                  {entry.user_id ? (
                    <div className='text-muted-foreground'>
                      User #{entry.user_id} · Token #{entry.token_id}
                    </div>
                  ) : null}
                </div>
              ),
            },
            {
              id: 'scope',
              header: t('Scope'),
              cell: (entry) =>
                [entry.rule_name, entry.using_group, entry.model_name]
                  .filter(Boolean)
                  .join(' / ') || '-',
            },
            {
              id: 'channel',
              header: t('Channel'),
              cell: (entry) =>
                `#${entry.channel_id}${entry.channel_name ? ` ${entry.channel_name}` : ''}`,
            },
            {
              id: 'ttl',
              header: t('TTL remaining'),
              cell: (entry) =>
                entry.ttl_seconds > 0 ? `${entry.ttl_seconds}s` : '-',
            },
            {
              id: 'actions',
              header: t('Actions'),
              className: 'text-right',
              cellClassName: 'text-right',
              cell: (entry) => (
                <div className='flex justify-end gap-1'>
                  <Button
                    variant='ghost'
                    size='icon'
                    className='h-7 w-7'
                    onClick={() => openEdit(entry)}
                    title={t('Edit')}
                  >
                    <Edit className='h-3 w-3' />
                  </Button>
                  <Button
                    variant='ghost'
                    size='icon'
                    className='h-7 w-7'
                    onClick={() => setDeleting(entry)}
                    title={t('Delete')}
                  >
                    <Trash2 className='h-3 w-3' />
                  </Button>
                </div>
              ),
            },
          ]}
        />
      </Dialog>

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete affinity cache entry?')}
        desc={deleting?.key_suffix || ''}
        destructive
        handleConfirm={() => deleting && void removeEntry(deleting)}
      />

      <Dialog
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        title={t('Edit Affinity Cache Entry')}
        contentClassName='sm:max-w-md'
        contentHeight='auto'
        footer={
          <>
            <Button variant='outline' onClick={() => setEditing(null)}>
              {t('Cancel')}
            </Button>
            <Button onClick={() => void saveEdit()} disabled={saving}>
              {saving ? t('Saving...') : t('Save')}
            </Button>
          </>
        }
      >
        <div className='space-y-4'>
          <div className='grid gap-1.5'>
            <Label>{t('Cache Key')}</Label>
            <div className='text-muted-foreground font-mono text-xs break-all'>
              {editing?.key}
            </div>
          </div>
          <div className='grid gap-1.5'>
            <Label htmlFor='channel-affinity-edit-channel'>
              {t('Channel ID')}
            </Label>
            <Input
              id='channel-affinity-edit-channel'
              type='number'
              min={1}
              value={channelID}
              onChange={(event) => setChannelID(event.target.value)}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label htmlFor='channel-affinity-edit-ttl'>
              {t('TTL (seconds)')}
            </Label>
            <Input
              id='channel-affinity-edit-ttl'
              type='number'
              min={1}
              value={ttlSeconds}
              onChange={(event) => setTtlSeconds(event.target.value)}
              placeholder={t('Use default TTL')}
            />
          </div>
        </div>
      </Dialog>
    </>
  )
}
