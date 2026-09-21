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
import { describe, expect, test } from 'vitest'

import { parseLogOther } from '../format'
import {
  isResponseModelMismatch,
  type ResponseModelObservation,
} from '../response-model'

function observation(
  returned: string,
  overrides: Partial<ResponseModelObservation> = {}
): ResponseModelObservation {
  return {
    requested_model: 'requested',
    upstream_model: 'mapped',
    returned_model: returned,
    ...overrides,
  }
}

describe('isResponseModelMismatch', () => {
  test('does not highlight an exact match with the requested model', () => {
    expect(isResponseModelMismatch(observation('requested'))).toBe(false)
  })

  test.each([
    ['mapped', 'mapped upstream name'],
    ['Requested', 'case-only difference'],
    ['requested-2026-09-01', 'dated request variant'],
    ['mapped-2026-09-01', 'dated upstream variant'],
    ['deepseek/requested', 'provider-qualified name'],
    ['vendor/MAPPED', 'provider-qualified case difference'],
    ['other', 'different model'],
    ['request', 'shorter name that the expected model extends'],
    ['vendor/other', 'different model behind a provider path'],
    ['vendor/request', 'provider path with a shorter name'],
    ['vendor/', 'provider path with no model segment'],
    ['vendor', 'provider segment alone'],
  ])('flags %s as a mismatch (%s)', (returned) => {
    expect(isResponseModelMismatch(observation(returned))).toBe(true)
  })

  test('compares against provider-qualified requested and upstream names as written', () => {
    expect(
      isResponseModelMismatch(
        observation('vendor/requested-2026-09-01', {
          requested_model: 'vendor/requested',
        })
      )
    ).toBe(true)
    expect(
      isResponseModelMismatch(
        observation('vendor/other', { requested_model: 'vendor/requested' })
      )
    ).toBe(true)
  })

  test('empty expected names never match', () => {
    expect(
      isResponseModelMismatch(
        observation('other', { requested_model: '', upstream_model: '' })
      )
    ).toBe(true)
  })

  test('returns false without an observation or a returned model', () => {
    expect(isResponseModelMismatch(undefined)).toBe(false)
    expect(isResponseModelMismatch(observation(''))).toBe(false)
  })
})

test('normalizes historical string response models before comparing against the requested name', () => {
  const other = parseLogOther(
    JSON.stringify({
      request_model: 'alias',
      upstream_model_name: 'gpt-5',
      response_model: 'gpt-5-2026',
    })
  )
  expect(other?.response_model).toEqual({
    requested_model: 'alias',
    upstream_model: 'gpt-5',
    returned_model: 'gpt-5-2026',
  })
  expect(isResponseModelMismatch(other?.response_model)).toBe(true)
  expect(parseLogOther('{"response_model":""}')?.response_model).toEqual({
    requested_model: '',
    upstream_model: '',
    returned_model: '',
  })
})
