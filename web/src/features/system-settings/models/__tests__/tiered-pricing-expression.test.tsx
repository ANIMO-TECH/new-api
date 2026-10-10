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
import { after, afterEach, test } from 'node:test'

import { Window } from 'happy-dom'

const domWindow = new Window()
const originalGlobals = new Map<string, PropertyDescriptor | undefined>()
for (const key of [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLInputElement',
  'HTMLTextAreaElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const) {
  originalGlobals.set(key, Object.getOwnPropertyDescriptor(globalThis, key))
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}
originalGlobals.set(
  'IS_REACT_ACT_ENVIRONMENT',
  Object.getOwnPropertyDescriptor(globalThis, 'IS_REACT_ACT_ENVIRONMENT')
)
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { TieredPricingEditor } = await import('../tiered-pricing-editor')
const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
  fallbackLng: 'en',
})
const container = document.createElement('div')
document.body.append(container)
let root = createRoot(container)
const published: string[] = []
const publishedRules: string[] = []
function renderEditor(
  expression: string,
  modelName = 'gpt-image-app',
  rules = ''
) {
  return act(() => {
    root.render(
      <I18nextProvider i18n={i18n}>
        <TieredPricingEditor
          modelName={modelName}
          billingExpr={expression}
          requestRuleExpr={rules}
          onBillingExprChange={(value) => published.push(value)}
          onRequestRuleExprChange={(value) => publishedRules.push(value)}
        />
      </I18nextProvider>
    )
  })
}
afterEach(async () => {
  await act(() => root.unmount())
  container.replaceChildren()
  root = createRoot(container)
  published.length = 0
  publishedRules.length = 0
})

test('opening a divided image expression preserves the original value without publishing zero pricing', async () => {
  const expression = '(p * 5 + img * 8 + c * 30) / 20'
  await renderEditor(expression)
  assert.deepEqual(published, [])
  assert.equal(container.querySelector('textarea')?.value, expression)
})

test('opening a visual tier preserves its stored expression without publishing a normalized draft', async () => {
  const expression = 'tier("base", p * 3 + c * 15)'
  await renderEditor(expression)
  assert.deepEqual(published, [])
  assert.ok(
    [...container.querySelectorAll('input')].some(
      (input) => input.value === 'base'
    )
  )
})

test('switching models loads the new expression without writing the previous model into the draft', async () => {
  await renderEditor('tier("base", p * 3 + c * 15)', 'agent-advoo')
  const expression = '(p * 5 + img * 8 + c * 30) / 20'
  await renderEditor(expression, 'gpt-image-app')
  assert.equal(container.querySelector('textarea')?.value, expression)
  assert.deepEqual(published, [])
})

test('editing raw pricing publishes the user input', async () => {
  const expression = '(p * 5 + img * 8 + c * 30) / 20'
  await renderEditor(expression)
  published.length = 0
  const input = container.querySelector('textarea')
  assert.ok(input)
  const next = '(p * 5 + img * 8 + c * 30) / 10'
  await act(() => {
    Object.getOwnPropertyDescriptor(
      domWindow.HTMLTextAreaElement.prototype,
      'value'
    )?.set?.call(input, next)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  assert.equal(published.at(-1), next)
  assert.equal(publishedRules.at(-1), '')
})

test('editing a visual price publishes the changed expression', async () => {
  await renderEditor('tier("base", p * 3 + c * 15)')
  const label = [...container.querySelectorAll('label')].find(
    (item) => item.textContent === 'Input price'
  )
  const input = label?.parentElement?.querySelector('input')
  assert.ok(input)
  await act(() => {
    Object.getOwnPropertyDescriptor(
      domWindow.HTMLInputElement.prototype,
      'value'
    )?.set?.call(input, '4')
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('focusout', { bubbles: true }))
  })
  assert.equal(published.at(-1), 'tier("base", p * 4 + c * 15)')
})

test('switching an unsupported expression to visual mode keeps the original expression and publishes nothing', async () => {
  const expression = '(p * 5 + img * 8 + c * 30) / 20'
  await renderEditor(expression)
  await act(async () => {
    ;(container.querySelector('[role="combobox"]') as HTMLElement).click()
    await domWindow.happyDOM.whenAsyncComplete()
  })
  const option = [...document.querySelectorAll('[role="option"]')].find(
    (item) => item.textContent === 'Visual editor'
  ) as HTMLElement
  assert.ok(option)
  await act(async () => {
    option.click()
    await domWindow.happyDOM.whenAsyncComplete()
  })
  assert.equal(container.querySelector('textarea')?.value, expression)
  assert.deepEqual(published, [])
})

test('opening an expression retains existing request multipliers without publishing new rules', async () => {
  const expression = '(p * 5 + img * 8 + c * 30) / 20'
  const rules = 'hour("UTC") >= 9 ? 0.5 : 1'
  await renderEditor(expression, 'gpt-image-app', rules)
  assert.ok(container.querySelector('textarea')?.value.includes(rules))
  assert.deepEqual(published, [])
  assert.deepEqual(publishedRules, [])
})

test('opening an empty expression does not publish a default zero price', async () => {
  await renderEditor('')
  assert.deepEqual(published, [])
  assert.deepEqual(publishedRules, [])
})

after(async () => {
  await act(() => root.unmount())
  container.remove()
  await domWindow.happyDOM.abort()
  for (const [key, descriptor] of originalGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor)
    else Reflect.deleteProperty(globalThis, key)
  }
})
