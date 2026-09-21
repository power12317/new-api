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
import { useTranslation } from 'react-i18next'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { LogOtherData } from '../types'
import { DetailRow } from './dialogs/log-detail-layout'

export function LogUpstreamStatus(props: { other: LogOtherData | null }) {
  const { t } = useTranslation()
  const status = props.other?.upstream_request_status
  const code = props.other?.upstream_status_code
  let label = t('Not recorded')
  let variant: StatusVariant = 'neutral'
  if (status === 'normal') {
    label = t('Normal')
    variant = 'success'
  } else if (status === 'degraded') {
    label = t('Degraded')
    variant = 'warning'
  } else if (status === 'error') {
    label = t('Error')
    variant = 'danger'
  } else if (code) {
    label = `HTTP ${code}`
  }
  return (
    <div className='flex w-fit flex-col gap-0.5'>
      <StatusBadge label={label} variant={variant} size='sm' copyable={false} />
      {status === 'error' && code ? (
        <span className='text-muted-foreground text-xs tabular-nums'>
          HTTP {code}
        </span>
      ) : null}
    </div>
  )
}

export function UpstreamResponseDetails(props: { other: LogOtherData | null }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const other = props.other
  return (
    <>
      <DetailRow
        label={t('Status')}
        value={<LogUpstreamStatus other={other} />}
      />
      {other?.upstream_status_code != null && (
        <DetailRow
          label={t('Upstream HTTP Status')}
          value={other.upstream_status_code}
          mono
        />
      )}
      {other?.upstream_request_status === 'degraded' && (
        <>
          <DetailRow
            label={t('Turn State Source')}
            value={
              other.turn_state_source === 'response'
                ? t('Response header')
                : t('Request header')
            }
          />
          {other.turn_state_length != null && (
            <DetailRow
              label={t('Turn State Length')}
              value={t('{{length}} bytes', {
                length: formatNumber(other.turn_state_length, locale),
              })}
              mono
            />
          )}
        </>
      )}
    </>
  )
}
