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
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { isResponseModelMismatch } from '../lib/response-model'
import type { LogOtherData } from '../types'
import { ModelBadge } from './model-badge'

export function LogModelDisplay(props: {
  modelName: string
  reasoningEffort?: string
  responseModel?: LogOtherData['response_model']
  onInspect?: () => void
}) {
  const { t } = useTranslation()
  const returned = props.responseModel?.returned_model
  const mismatch = isResponseModelMismatch(props.responseModel)
  const badge = (
    <ModelBadge modelName={props.modelName} truncateText copyable={false} />
  )

  return (
    <div className='flex max-w-full min-w-0 flex-col gap-1'>
      <div className='flex min-w-0 items-center gap-1'>
        {props.onInspect ? (
          <Button
            variant='ghost'
            aria-label={`${t('Model')}: ${props.modelName}`}
            aria-haspopup='dialog'
            onClick={props.onInspect}
            className='h-auto min-h-8 min-w-0 justify-start px-0 py-0 font-normal'
          >
            {badge}
          </Button>
        ) : (
          <CopyButton
            value={props.modelName}
            aria-label={`${t('Copy')}: ${props.modelName}`}
            size='sm'
            iconClassName='hidden'
            className='h-auto min-h-6 min-w-0 shrink justify-start px-0 py-0 font-normal'
          >
            {badge}
          </CopyButton>
        )}
        {props.reasoningEffort && (
          <StatusBadge
            label={props.reasoningEffort}
            aria-label={`${t('Request Reasoning Effort')}: ${props.reasoningEffort}`}
            variant='neutral'
            size='sm'
            copyable={false}
            showDot={false}
            className='shrink-0'
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
        {returned ? (
          <>
            <CopyButton
              value={returned}
              aria-label={`${t('Copy')}: ${returned}`}
              size='sm'
              iconClassName='size-3'
              className='h-auto min-h-6 min-w-0 shrink justify-start gap-1 px-0 py-0 font-mono text-xs font-normal'
            >
              <span className='max-w-64 min-w-0 truncate'>{returned}</span>
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
            {props.responseModel ? t('Not returned') : t('Not recorded')}
          </span>
        )}
      </div>
    </div>
  )
}
