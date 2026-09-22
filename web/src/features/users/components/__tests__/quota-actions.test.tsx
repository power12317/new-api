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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { User } from '../../types'
import { DataTableRowActions } from '../data-table-row-actions'
import { UsersProvider } from '../users-provider'

const clients: QueryClient[] = []

function ActionsFixture(props: { target: User }) {
  const table = useReactTable({
    data: [props.target],
    columns: [],
    getCoreRowModel: getCoreRowModel(),
  })
  return <DataTableRowActions row={table.getRowModel().rows[0]} />
}

function renderActions(viewerRole: number, targetRole: number, targetId = 2) {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'operator', role: viewerRole })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: { success: true, data: [] },
  }))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const target: User = {
    id: targetId,
    username: 'target',
    display_name: 'Target',
    role: targetRole,
    status: 1,
    quota: 500000,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  }
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <UsersProvider>
          <ActionsFixture target={target} />
        </UsersProvider>
      </TooltipProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

it.each([
  ['self add', 10, 1, 'Add', 'add'],
  ['peer subtract', 10, 2, 'Subtract', 'subtract'],
  ['ordinary user override', 1, 2, 'Override', 'override'],
])(
  'opens and submits quota from the row menu for %s without loading the restricted profile',
  async (_, role, id, label, mode) => {
    const user = userEvent.setup()
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true } })
    renderActions(10, role, id)
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(
      await screen.findByRole('menuitem', { name: 'Adjust Quota' })
    )
    const dialog = await screen.findByRole('dialog', { name: 'Adjust Quota' })
    expect(dialog).toBeVisible()
    await user.click(within(dialog).getByRole('button', { name: label }))
    await user.type(within(dialog).getByRole('spinbutton'), '2')
    await user.click(within(dialog).getByRole('button', { name: 'Confirm' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledExactlyOnceWith('/api/user/manage', {
        id,
        action: 'add_quota',
        mode,
        value: 1000000,
      })
    )
    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', { name: 'Adjust Quota' })
      ).not.toBeInTheDocument()
    )
    expect(api.get).not.toHaveBeenCalledWith(`/api/user/${id}`)
  }
)

it.each([
  [10, 100],
  [1, 1],
])(
  'hides quota action from viewer role %s for target role %s',
  async (viewer, target) => {
    const user = userEvent.setup()
    renderActions(viewer, target)
    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await screen.findByRole('menu')
    expect(
      screen.queryByRole('menuitem', { name: 'Adjust Quota' })
    ).not.toBeInTheDocument()
  }
)

it('allows root quota management and opens the dialog with keyboard activation', async () => {
  const user = userEvent.setup()
  renderActions(100, 100)
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  const item = await screen.findByRole('menuitem', { name: 'Adjust Quota' })
  await user.keyboard('{Home}{ArrowDown}{ArrowDown}{ArrowDown}')
  await waitFor(() => expect(item).toHaveFocus())
  await user.keyboard('{Enter}')
  expect(
    await screen.findByRole('dialog', { name: 'Adjust Quota' })
  ).toBeVisible()
})
