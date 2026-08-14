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
import { z } from 'zod'

export const customHealthProbeSchema = z
  .object({
    enabled: z.boolean(),
    scanIntervalSeconds: z.coerce.number().int().min(15).max(3600),
    requestTimeoutSeconds: z.coerce.number().int().min(1).max(600),
    concurrency: z.coerce.number().int().min(1).max(20),
    initialDelaySeconds: z.coerce.number().int().min(1).max(86400),
    backoffMultiplier: z.coerce.number().min(1).max(10),
    maxBackoffSeconds: z.coerce.number().int().min(1).max(604800),
    maxAttempts: z.coerce.number().int().min(0).max(100000),
    notifyOnRecovery: z.boolean(),
    notifyOnExhausted: z.boolean(),
  })
  .superRefine((value, ctx) => {
    if (value.maxBackoffSeconds < value.initialDelaySeconds) {
      ctx.addIssue({
        code: 'custom',
        path: ['maxBackoffSeconds'],
        message:
          'Maximum backoff must be greater than or equal to the initial delay',
      })
    }
  })

export type CustomHealthProbeValues = z.infer<typeof customHealthProbeSchema>
export type CustomHealthProbeFormInput = z.input<typeof customHealthProbeSchema>

export const customHealthProbeOptionKeys: Record<
  keyof CustomHealthProbeValues,
  string
> = {
  enabled: 'health_probe_setting.enabled',
  scanIntervalSeconds: 'health_probe_setting.scan_interval_seconds',
  requestTimeoutSeconds: 'health_probe_setting.request_timeout_seconds',
  concurrency: 'health_probe_setting.concurrency',
  initialDelaySeconds: 'health_probe_setting.initial_delay_seconds',
  backoffMultiplier: 'health_probe_setting.backoff_multiplier',
  maxBackoffSeconds: 'health_probe_setting.max_backoff_seconds',
  maxAttempts: 'health_probe_setting.max_attempts',
  notifyOnRecovery: 'health_probe_setting.notify_on_recovery',
  notifyOnExhausted: 'health_probe_setting.notify_on_exhausted',
}
