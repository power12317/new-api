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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { GroupBadge } from '@/components/group-badge'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Switch } from '@/components/ui/switch'

import { getModelChannelConnections, setModelChannelStatus } from '../api'
import { modelsQueryKeys } from '../lib'
import type { Model, ModelChannelConnection } from '../types'
import { ModelSquareStatus } from './model-square-status'

export function ModelConnections(props: { model: Model }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const query = useQuery({
    queryKey: [
      ...modelsQueryKeys.all,
      'connections',
      props.model.model_name,
      props.model.name_rule,
    ],
    queryFn: () => getModelChannelConnections(props.model),
  })
  const mutation = useMutation({
    mutationFn: (change: {
      connection: ModelChannelConnection
      enabled: boolean
    }) => setModelChannelStatus(change.connection, change.enabled),
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({ queryKey: modelsQueryKeys.all }),
        client.invalidateQueries({ queryKey: ['pricing'] }),
        client.invalidateQueries({ queryKey: ['playground-models'] }),
      ])
      toast.success(t('Model channel status updated'))
    },
    meta: { errorToast: false },
  })
  const connections = query.data ?? []
  const groups = [
    ...new Set(
      connections
        .filter((channel) => channel.enabled && channel.channel_status === 1)
        .flatMap((channel) => channel.groups)
    ),
  ]
  if (query.isPending) return <LoadingState />
  if (query.isError) {
    return (
      <ErrorState
        title={t('Failed to load model channels')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <div className='min-h-0 flex-1 space-y-6 overflow-auto p-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Choose which channels can serve this model. Changes save immediately and do not affect other models on the channel. If all eligible channels are disabled, new requests return a model disabled error.'
        )}
      </p>
      <ModelSquareStatus model={props.model} detail />
      <section className='space-y-3'>
        <h3 className='font-medium'>{t('Bound Channels')}</h3>
        {connections.length ? (
          <StaticDataTable
            data={connections}
            columns={[
              {
                id: 'name',
                header: t('Channel'),
                cell: (channel) => (
                  <div className='min-w-0 space-y-1'>
                    <p className='break-all'>{channel.channel_name}</p>
                    {channel.channel_status !== 1 && (
                      <StatusBadge
                        label={t('Channel disabled')}
                        variant='neutral'
                        size='sm'
                      />
                    )}
                  </div>
                ),
              },
              {
                id: 'model',
                header: t('Model'),
                cell: (channel) => (
                  <span className='break-all'>{channel.model_name}</span>
                ),
              },
              {
                id: 'groups',
                header: t('Groups'),
                cell: (channel) => (
                  <div className='flex flex-wrap gap-1'>
                    {channel.groups.map((group) => (
                      <GroupBadge key={group} group={group} />
                    ))}
                  </div>
                ),
              },
              {
                id: 'enabled',
                header: t('Allow invocation'),
                cell: (channel) => (
                  <div className='space-y-1'>
                    <Switch
                      checked={channel.enabled}
                      disabled={
                        mutation.isPending ||
                        (!channel.enabled && !channel.directly_disabled)
                      }
                      aria-label={t('Allow {{model}} on {{channel}}', {
                        model: channel.model_name,
                        channel: channel.channel_name,
                      })}
                      onCheckedChange={(enabled) =>
                        mutation.mutate({ connection: channel, enabled })
                      }
                    />
                    {!channel.enabled && !channel.directly_disabled && (
                      <p className='text-muted-foreground text-xs'>
                        {t('Disabled by a mapped model on this channel')}
                      </p>
                    )}
                  </div>
                ),
              },
            ]}
          />
        ) : (
          <EmptyState title={t('No configured channels')} />
        )}
        {mutation.isError && (
          <p role='alert' className='text-destructive text-sm'>
            {t('Failed to update model channel status')}:{' '}
            {t(mutation.error.message)}
          </p>
        )}
      </section>
      <section className='space-y-3'>
        <h3 className='font-medium'>{t('Enable Groups')}</h3>
        <div className='flex flex-wrap gap-2'>
          {groups.map((group) => (
            <GroupBadge key={group} group={group} />
          ))}
          {!groups.length && (
            <span className='text-muted-foreground text-sm'>
              {t('No enabled groups')}
            </span>
          )}
        </div>
      </section>
      <section className='space-y-3'>
        <h3 className='font-medium'>{t('Supported endpoints')}</h3>
        <div className='flex flex-wrap gap-2'>
          {props.model.supported_endpoints?.map((endpoint) => (
            <StatusBadge key={endpoint} variant='neutral' label={endpoint} />
          ))}
          {!props.model.supported_endpoints?.length && (
            <span className='text-muted-foreground text-sm'>
              {t('No endpoints inferred from channels')}
            </span>
          )}
        </div>
      </section>
      {props.model.name_rule !== 0 && (
        <section className='space-y-3'>
          <h3 className='font-medium'>{t('Matched models')}</h3>
          <div className='flex flex-wrap gap-2'>
            {props.model.matched_models?.map((name) => (
              <StatusBadge key={name} variant='neutral' label={name} />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
