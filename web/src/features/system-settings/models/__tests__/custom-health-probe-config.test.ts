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

import {
  customHealthProbeOptionKeys,
  customHealthProbeSchema,
} from '../custom-health-probe-config'

const valid = {
  enabled: true,
  scanIntervalSeconds: 30,
  requestTimeoutSeconds: 120,
  concurrency: 1,
  initialDelaySeconds: 60,
  backoffMultiplier: 2,
  maxBackoffSeconds: 3600,
  maxAttempts: 0,
  notifyOnRecovery: true,
  notifyOnExhausted: true,
}

describe('Advoo health probe settings', () => {
  test('accepts unlimited retries and maps every value to an isolated namespace', () => {
    assert.equal(customHealthProbeSchema.safeParse(valid).success, true)
    assert.equal(
      Object.values(customHealthProbeOptionKeys).every((key) =>
        key.startsWith('health_probe_setting.')
      ),
      true
    )
  })

  test('rejects unsafe concurrency and a maximum backoff below the initial delay', () => {
    assert.equal(
      customHealthProbeSchema.safeParse({ ...valid, concurrency: 21 }).success,
      false
    )
    assert.equal(
      customHealthProbeSchema.safeParse({
        ...valid,
        initialDelaySeconds: 120,
        maxBackoffSeconds: 60,
      }).success,
      false
    )
  })
})
