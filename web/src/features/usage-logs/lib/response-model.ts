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
import type { LogOtherData } from '../types'

export type ResponseModelObservation = NonNullable<
  LogOtherData['response_model']
>

/**
 * Highlight any difference from the client's requested name, including case,
 * mapped names and dated variants. This is a display comparison, not a change
 * to provider routing or the backend's response observation retention policy.
 */
export function isResponseModelMismatch(
  observation: ResponseModelObservation | undefined
): boolean {
  if (!observation) return false
  const returned = observation.returned_model ?? ''
  if (returned.trim() === '') return false
  return returned !== observation.requested_model
}
