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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { channelSchema } from '../../types'
import {
  buildSettingJSON,
  channelFormSchema,
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
} from '../channel-form'

const channel = channelSchema.parse({
  id: 1,
  type: 1,
  name: 'Billing test',
  key: 'test-only',
  status: 1,
  created_time: 0,
  test_time: 0,
  response_time: 0,
  balance_updated_time: 0,
  channel_info: {},
  settings: '{}',
})

const form = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Billing test',
  type: 1,
  key: 'test-only',
  models: 'gpt-test',
}

describe('channel billing ratio', () => {
  test('blank and 1x settings preserve default billing without storing an override', () => {
    for (const billing_ratio of [undefined, 1]) {
      const parsed = channelFormSchema.parse({ ...form, billing_ratio })
      assert.equal(
        JSON.parse(buildSettingJSON(parsed)).billing_ratio,
        undefined
      )
    }
  })
  test('two-decimal discounts and premiums round trip through saved channel settings', () => {
    for (const billing_ratio of [0.01, 0.5, 1.25, 2.55, 1000]) {
      const parsed = channelFormSchema.parse({ ...form, billing_ratio })
      const setting = buildSettingJSON(parsed)
      assert.equal(JSON.parse(setting).billing_ratio, billing_ratio)
      assert.equal(
        transformChannelToFormDefaults({ ...channel, setting }).billing_ratio,
        billing_ratio
      )
    }
  })
  test('zero, negative, excessive and over-precision multipliers cannot be submitted', () => {
    for (const billing_ratio of [
      0,
      -1,
      1000.01,
      1.001,
      1.00000000001,
      Number.POSITIVE_INFINITY,
      Number.NaN,
    ]) {
      assert.equal(
        channelFormSchema.safeParse({ ...form, billing_ratio }).success,
        false
      )
    }
  })
  test('existing channels without a multiplier open with the optional input blank', () => {
    assert.equal(
      transformChannelToFormDefaults({ ...channel, setting: '{}' })
        .billing_ratio,
      undefined
    )
  })
})
