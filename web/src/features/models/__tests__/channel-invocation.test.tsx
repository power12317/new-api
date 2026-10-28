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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ModelConnections } from '../components/model-connections'
import type { Model, ModelChannelConnection } from '../types'

const model: Model = {
  id: 0,
  model_name: 'channel-only',
  name_rule: 0,
  status: 1,
  sync_official: 0,
  created_time: 0,
  updated_time: 0,
}
const clients: QueryClient[] = []

afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <ModelConnections model={model} />
    </QueryClientProvider>
  )
}

function connections(): ModelChannelConnection[] {
  return [
    {
      channel_id: 1,
      channel_name: 'Primary',
      channel_type: 1,
      channel_status: 1,
      model_name: 'channel-only',
      groups: ['default'],
      enabled: true,
      directly_disabled: false,
    },
    {
      channel_id: 2,
      channel_name: 'Fallback',
      channel_type: 1,
      channel_status: 1,
      model_name: 'channel-only',
      groups: ['default'],
      enabled: false,
      directly_disabled: true,
    },
  ]
}

it('updates only the selected model and channel, including rows with no metadata', async () => {
  const stored = connections()
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: stored },
  })
  const put = vi.spyOn(api, 'put').mockImplementation(async (_url, payload) => {
    const change = payload as { channel_id: number; enabled: boolean }
    stored[change.channel_id - 1] = {
      ...stored[change.channel_id - 1],
      enabled: change.enabled,
      directly_disabled: !change.enabled,
    }
    return { data: { success: true } }
  })
  mount()
  const user = userEvent.setup()
  const primary = await screen.findByRole('switch', {
    name: 'Allow channel-only on Primary',
  })
  expect(primary).toBeChecked()
  expect(
    screen.getByRole('switch', { name: 'Allow channel-only on Fallback' })
  ).not.toBeChecked()
  await user.click(primary)
  await waitFor(() => expect(primary).not.toBeChecked())
  expect(put).toHaveBeenLastCalledWith('/api/models/channel_status', {
    channel_id: 1,
    model_name: 'channel-only',
    enabled: false,
  })
  expect(
    screen.getByRole('switch', { name: 'Allow channel-only on Fallback' })
  ).not.toBeChecked()
  screen.getByRole('switch', { name: 'Allow channel-only on Fallback' }).focus()
  await user.keyboard(' ')
  await waitFor(() =>
    expect(
      screen.getByRole('switch', { name: 'Allow channel-only on Fallback' })
    ).toBeChecked()
  )
  expect(put).toHaveBeenLastCalledWith('/api/models/channel_status', {
    channel_id: 2,
    model_name: 'channel-only',
    enabled: true,
  })
})

it('keeps the saved switch state and exposes an error when a write fails', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: connections() },
  })
  vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: false,
      message:
        'The model is no longer configured on this channel. Reload the model list.',
    },
  })
  mount()
  const primary = await screen.findByRole('switch', {
    name: 'Allow channel-only on Primary',
  })
  await userEvent.setup().click(primary)
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'The model is no longer configured on this channel.'
  )
  expect(primary).toBeChecked()
  expect(primary).toBeEnabled()
})

it('displays mapping restrictions and prevents enabling an alias while its target is disabled', async () => {
  const stored = connections()
  stored[0] = { ...stored[0], enabled: false, directly_disabled: false }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: stored },
  })
  const put = vi.spyOn(api, 'put')
  mount()
  expect(
    await screen.findByRole('switch', { name: 'Allow channel-only on Primary' })
  ).toHaveAttribute('aria-disabled', 'true')
  expect(
    screen.getByText('Disabled by a mapped model on this channel')
  ).toBeVisible()
  expect(put).not.toHaveBeenCalled()
})

it('shows a retry action after loading fails and then displays the channels', async () => {
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('unavailable'))
    .mockResolvedValue({ data: { success: true, data: connections() } })
  mount()
  await userEvent
    .setup()
    .click(await screen.findByRole('button', { name: 'Retry' }))
  expect(
    await screen.findByRole('switch', { name: 'Allow channel-only on Primary' })
  ).toBeChecked()
})
