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
import { afterEach, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { Route as SystemInfoRoute } from '../system-info'
import { Route as SystemSettingsRoute } from '../system-settings/route'
import { Route as TaskPluginsRoute } from '../task-plugins'

afterEach(() => useAuthStore.getState().auth.reset())

it.each([undefined, 1, 10, 100])(
  'management pages enforce the administrator boundary for role %s',
  async (role) => {
    if (role !== undefined) {
      useAuthStore.getState().auth.setUser({ id: 1, username: 'viewer', role })
    }
    for (const route of [
      SystemInfoRoute,
      SystemSettingsRoute,
      TaskPluginsRoute,
    ]) {
      const beforeLoad = route.options.beforeLoad
      if (typeof beforeLoad !== 'function') {
        throw new Error('Missing route guard')
      }
      const load = beforeLoad as () => unknown
      if (role !== undefined && role >= 10) {
        expect(load).not.toThrow()
      } else {
        expect(load).toThrow()
      }
    }
  }
)
