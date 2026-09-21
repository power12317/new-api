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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen, within } from '@testing-library/react'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { afterEach, expect, test } from 'vitest'

import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { DetailsDialog } from '../dialogs/details-dialog'
import { LogUpstreamStatus } from '../log-upstream-status'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider } from '../usage-logs-provider'

const clients: QueryClient[] = []

function Fixture(props: {
  log: UsageLog
  details?: boolean
  mobile?: boolean
  hideStatus?: boolean
}) {
  const table = useReactTable({
    data: [props.log],
    columns: useCommonLogsColumns(false, false),
    getCoreRowModel: getCoreRowModel(),
    state: { columnVisibility: { upstream_status: !props.hideStatus } },
  })
  if (props.mobile) {
    return <UsageLogsMobileList table={table} logCategory='common' />
  }
  if (props.details) {
    return (
      <DetailsDialog
        log={props.log}
        isAdmin={false}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    )
  }
  return (
    <table>
      <tbody>
        <tr>
          {table
            .getRowModel()
            .rows[0].getAllCells()
            .filter((cell) =>
              ['model_name', 'upstream_status'].includes(cell.column.id)
            )
            .map((cell) => (
              <td key={cell.id} aria-label={cell.column.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </td>
            ))}
        </tr>
      </tbody>
    </table>
  )
}

function renderLog(
  other: Record<string, unknown>,
  options: {
    details?: boolean
    type?: number
    mobile?: boolean
    hideStatus?: boolean
  } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {}, { updatedAt: Date.now() + 60_000 })
  clients.push(client)
  const log = usageLogSchema.parse({
    id: 1,
    user_id: 1,
    created_at: 1,
    type: options.type ?? 2,
    content: '',
    model_name: 'requested-model',
    other: JSON.stringify(other),
  })
  return render(
    <QueryClientProvider client={client}>
      <UsageLogsProvider>
        <Fixture log={log} {...options} />
      </UsageLogsProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
})

test.each([
  {
    other: { upstream_status_code: 200, upstream_request_status: 'normal' },
    expected: 'Normal',
  },
  {
    other: {
      upstream_status_code: 200,
      upstream_request_status: 'degraded',
      turn_state_source: 'response',
      turn_state_length: 312,
    },
    expected: 'Degraded',
  },
  {
    other: { upstream_status_code: 503, upstream_request_status: 'error' },
    expected: 'Error',
  },
  { other: {}, expected: 'Not recorded' },
])('shows recorded upstream status: $expected', ({ other, expected }) => {
  renderLog(other)
  expect(
    within(screen.getByRole('cell', { name: 'upstream_status' })).getByText(
      expected
    )
  ).toBeVisible()
})

test.each([undefined, 0, 292, 332, 17])(
  'omits turn-state explanations for normal responses with length %s',
  (length) => {
    renderLog(
      {
        upstream_status_code: 200,
        upstream_request_status: 'normal',
        turn_state_length: length,
        turn_state_source: length === undefined ? 'none' : 'response',
      },
      { details: true }
    )
    expect(screen.getByText('Normal')).toBeVisible()
    expect(screen.queryByText('Turn State Source')).not.toBeInTheDocument()
    expect(screen.queryByText('Turn State Length')).not.toBeInTheDocument()
  }
)

test('shows official model mismatch evidence alongside the degraded status', () => {
  renderLog({
    response_model: {
      requested_model: 'requested-model',
      upstream_model: 'mapped-model',
      returned_model: 'different-model',
    },
    upstream_status_code: 200,
    upstream_request_status: 'degraded',
    turn_state_length: 312,
    turn_state_source: 'response',
  })
  expect(screen.getByText('Response model: different-model')).toBeVisible()
  expect(
    within(screen.getByRole('cell', { name: 'upstream_status' })).getByText(
      'Degraded'
    )
  ).toBeVisible()
})

test('shows the source and length explaining a degraded response', () => {
  renderLog(
    {
      upstream_status_code: 200,
      upstream_request_status: 'degraded',
      turn_state_source: 'request',
      turn_state_length: 356,
    },
    { details: true }
  )
  expect(screen.getByText('Degraded')).toBeVisible()
  expect(screen.getByText('Request header')).toBeVisible()
  expect(screen.getByText('356 bytes')).toBeVisible()
})

test('mobile cards show the recorded degraded status', () => {
  renderLog(
    { upstream_status_code: 200, upstream_request_status: 'degraded' },
    { mobile: true }
  )
  expect(screen.getByText('Degraded')).toBeVisible()
})

test('hiding the status column also hides its mobile badge', () => {
  renderLog(
    { upstream_request_status: 'degraded' },
    { mobile: true, hideStatus: true }
  )
  expect(screen.queryByText('Degraded')).not.toBeInTheDocument()
})

test.each([1, 3, 6, 7])(
  'non-request log type %s does not show an upstream status',
  (type) => {
    renderLog({ upstream_request_status: 'degraded' }, { type })
    expect(screen.queryByText('Degraded')).not.toBeInTheDocument()
  }
)

test('status labels react to language changes', async () => {
  const language = createInstance()
  await language.init({
    lng: 'en',
    resources: { en, zh },
    interpolation: { escapeValue: false },
  })
  const other = {
    upstream_request_status: 'degraded' as const,
  }
  const { rerender } = render(
    <I18nextProvider i18n={language}>
      <LogUpstreamStatus other={other} />
    </I18nextProvider>
  )
  expect(screen.getByText('Degraded')).toBeVisible()
  await language.changeLanguage('zh')
  rerender(
    <I18nextProvider i18n={language}>
      <LogUpstreamStatus other={other} />
    </I18nextProvider>
  )
  expect(screen.getByText('降智')).toBeVisible()
})
