/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import {
  customHealthProbeOptionKeys,
  customHealthProbeSchema,
  type CustomHealthProbeFormInput,
  type CustomHealthProbeValues,
} from './custom-health-probe-config'

type Props = {
  defaultValues: CustomHealthProbeValues & {
    officialAutoRecoveryEnabled: boolean
  }
}

const numberFields: Array<{
  name: keyof Pick<
    CustomHealthProbeValues,
    | 'scanIntervalSeconds'
    | 'requestTimeoutSeconds'
    | 'concurrency'
    | 'initialDelaySeconds'
    | 'backoffMultiplier'
    | 'maxBackoffSeconds'
    | 'maxAttempts'
  >
  label: string
  description: string
  min: number
  max: number
  step?: number
}> = [
  {
    name: 'scanIntervalSeconds',
    label: 'Scan interval (seconds)',
    description: 'How often the scheduler looks for due recovery probes.',
    min: 15,
    max: 3600,
  },
  {
    name: 'requestTimeoutSeconds',
    label: 'Request timeout (seconds)',
    description:
      'Maximum duration of one probe request, including image generation.',
    min: 1,
    max: 600,
  },
  {
    name: 'concurrency',
    label: 'Probe concurrency',
    description: 'Maximum number of billable probes running at once.',
    min: 1,
    max: 20,
  },
  {
    name: 'initialDelaySeconds',
    label: 'Initial delay (seconds)',
    description:
      'Delay between a real-request auto-disable and the first probe.',
    min: 1,
    max: 86400,
  },
  {
    name: 'backoffMultiplier',
    label: 'Backoff multiplier',
    description: 'Multiplier applied after each failed probe.',
    min: 1,
    max: 10,
    step: 0.1,
  },
  {
    name: 'maxBackoffSeconds',
    label: 'Maximum backoff (seconds)',
    description: 'Upper bound for the delay between probes.',
    min: 1,
    max: 604800,
  },
  {
    name: 'maxAttempts',
    label: 'Maximum attempts',
    description: 'Use 0 to keep retrying indefinitely.',
    min: 0,
    max: 100000,
  },
]

export function CustomHealthProbeSection({ defaultValues }: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaults = useMemo<CustomHealthProbeValues>(
    () => ({
      enabled: defaultValues.enabled ?? false,
      scanIntervalSeconds: defaultValues.scanIntervalSeconds ?? 30,
      requestTimeoutSeconds: defaultValues.requestTimeoutSeconds ?? 120,
      concurrency: defaultValues.concurrency ?? 1,
      initialDelaySeconds: defaultValues.initialDelaySeconds ?? 60,
      backoffMultiplier: defaultValues.backoffMultiplier ?? 2,
      maxBackoffSeconds: defaultValues.maxBackoffSeconds ?? 3600,
      maxAttempts: defaultValues.maxAttempts ?? 0,
      notifyOnRecovery: defaultValues.notifyOnRecovery ?? true,
      notifyOnExhausted: defaultValues.notifyOnExhausted ?? true,
    }),
    [defaultValues]
  )
  const baselineRef = useRef(defaults)
  const form = useForm<
    CustomHealthProbeFormInput,
    unknown,
    CustomHealthProbeValues
  >({ resolver: zodResolver(customHealthProbeSchema), defaultValues: defaults })
  useResetForm(form, defaults)

  const onSubmit = async (values: CustomHealthProbeValues) => {
    const changed = (
      Object.keys(values) as Array<keyof CustomHealthProbeValues>
    ).filter((key) => values[key] !== baselineRef.current[key])
    if (changed.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const key of changed) {
      await updateOption.mutateAsync({
        key: customHealthProbeOptionKeys[key],
        value: values[key],
      })
    }
    baselineRef.current = values
  }

  return (
    <SettingsSection title={t('Advoo Health Probe')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <Alert>
            <AlertTitle>{t('Advoo custom extension')}</AlertTitle>
            <AlertDescription>
              {t(
                'This recovery mechanism is maintained separately from all official new-api channel test modes. When enabled, official automatic recovery is suppressed.'
              )}
              {defaultValues.officialAutoRecoveryEnabled
                ? ` ${t('The official re-enable option is currently on, but Advoo recovery takes precedence while this extension is enabled.')}`
                : null}
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Advoo health probe')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Probe only channels or keys disabled by real request failures.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          {numberFields.map((config) => (
            <FormField
              key={config.name}
              control={form.control}
              name={config.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t(config.label)}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={config.min}
                      max={config.max}
                      step={config.step ?? 1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>{t(config.description)}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}

          <FormField
            control={form.control}
            name='notifyOnRecovery'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Notify on recovery')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Notify the root user when a probe restores a channel or key.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <FormField
            control={form.control}
            name='notifyOnExhausted'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Notify when attempts are exhausted')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Only applies when maximum attempts is greater than zero.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
