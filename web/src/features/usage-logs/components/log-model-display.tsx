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

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { cn } from '@/lib/utils'

import type { LogOtherData } from '../types'
import { ModelBadge } from './model-badge'

// Compose the existing model badge and copy interaction for desktop and mobile.
export function LogModelDisplay(props: {
  modelName: string
  other: LogOtherData | null
  wrapText?: boolean
  onInspect?: () => void
}) {
  const { t } = useTranslation()
  const responseModel = props.other?.response_model
  const effort = props.other?.request_reasoning_effort
  const mismatch = !!responseModel && responseModel !== props.modelName

  return (
    <div className='flex max-w-full min-w-0 flex-col gap-1'>
      <div className='flex min-w-0 items-center gap-1'>
        <ModelBadge
          modelName={props.modelName}
          truncateText
          onInspect={props.onInspect}
        />
        {effort && (
          <StatusBadge
            label={effort}
            variant='neutral'
            size='sm'
            copyable={false}
            showDot={false}
            className='shrink-0'
            aria-label={`${t('Request Reasoning Effort')}: ${effort}`}
          />
        )}
      </div>
      <div
        role='group'
        aria-label={t('Response Model')}
        className={cn(
          'flex min-w-0 items-center gap-1 text-xs',
          mismatch ? 'text-warning' : 'text-muted-foreground'
        )}
      >
        <span aria-hidden='true'>↳</span>
        {responseModel ? (
          <>
            <CopyButton
              value={responseModel}
              size='sm'
              className='h-auto min-h-6 min-w-0 shrink justify-start gap-1 px-0 py-0 font-mono text-xs font-normal whitespace-normal'
              iconClassName='size-3'
              aria-label={`${t('Copy')}: ${responseModel}`}
            >
              <span
                className={cn(
                  'min-w-0',
                  'truncate',
                  !props.wrapText && 'max-w-64'
                )}
              >
                {responseModel}
              </span>
            </CopyButton>
            {mismatch && (
              <StatusBadge
                label={t('Model mismatch')}
                variant='warning'
                size='sm'
                copyable={false}
                showDot={false}
              />
            )}
          </>
        ) : (
          <span>
            {responseModel === undefined
              ? t('Not recorded')
              : t('Not returned')}
          </span>
        )}
      </div>
    </div>
  )
}
