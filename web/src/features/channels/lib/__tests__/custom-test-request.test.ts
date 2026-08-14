/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from '../channel-form'

const form = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Image channel',
  key: 'secret',
  models: 'gpt-image-2',
  test_model: 'gpt-image-2',
  test_endpoint_type: 'image-generation' as const,
  test_request_body: JSON.stringify({
    prompt: 'blue square',
    quality: 'low',
    n: 1,
  }),
}

describe('Advoo custom channel test request', () => {
  test('validates and preserves endpoint and body in create and update payloads', () => {
    const parsed = channelFormSchema.parse(form)
    const created = transformFormDataToCreatePayload(parsed).channel
    const updated = transformFormDataToUpdatePayload(parsed, 42)

    assert.equal(created.test_endpoint_type, 'image-generation')
    assert.equal(created.test_request_body, form.test_request_body)
    assert.equal(updated.test_endpoint_type, 'image-generation')
    assert.equal(updated.test_request_body, form.test_request_body)
  })

  test('serializes automatic endpoint as an explicit clear on update', () => {
    const parsed = channelFormSchema.parse({
      ...form,
      test_endpoint_type: 'auto',
      test_request_body: '',
    })
    const updated = transformFormDataToUpdatePayload(parsed, 42)
    assert.equal(updated.test_endpoint_type, '')
    assert.equal(updated.test_request_body, '')
  })

  test('rejects arrays and bodies larger than 64 KiB', () => {
    assert.equal(
      channelFormSchema.safeParse({ ...form, test_request_body: '[]' }).success,
      false
    )
    assert.equal(
      channelFormSchema.safeParse({
        ...form,
        test_request_body: JSON.stringify({ value: 'x'.repeat(65536) }),
      }).success,
      false
    )
  })
})
