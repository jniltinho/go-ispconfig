<script setup lang="ts">
// ISPConfig-style tabbed form: flat tabs on top of a white card, fields
// rendered from JSON form metadata, labels on the left with an automatic ':',
// green Save / Cancel buttons and per-field errors in alert-danger style.
import { reactive, ref, watch } from 'vue'
import UiAlert from './UiAlert.vue'
import { useI18n } from '../i18n'

export interface FormField {
  name: string
  type: 'text' | 'password' | 'textarea' | 'select' | 'checkbox' | 'checkboxarray' | 'legend'
  label: string
  options?: { value: string; label: string }[]
  default?: unknown
  /** readonly renders the field disabled (e.g. server-managed serial). */
  readonly?: boolean
  /** collapsible folds the legend's section into a <details> (DKIM block). */
  collapsible?: boolean
}

/** Section is a legend and the fields following it, up to the next legend. */
interface Section {
  legend?: FormField
  collapsible: boolean
  fields: FormField[]
}

export interface FormTab {
  name: string
  label: string
  fields: FormField[]
}

export interface FormMetadata {
  tabs: FormTab[]
}

const props = defineProps<{
  metadata: FormMetadata
  modelValue?: Record<string, unknown>
  errors?: Record<string, string[]>
  /** saving disables Save/Cancel while the request is in flight. */
  saving?: boolean
}>()

const emit = defineEmits<{
  (e: 'save', values: Record<string, unknown>): void
  (e: 'cancel'): void
  (e: 'values-change', values: Record<string, unknown>): void
}>()

const { t } = useI18n()
const activeTab = ref(props.metadata.tabs[0]?.name ?? '')

// Working copy of the values: modelValue > field default.
const values = reactive<Record<string, unknown>>({})
for (const tab of props.metadata.tabs) {
  for (const field of tab.fields) {
    if (field.type === 'legend') continue
    values[field.name] =
      props.modelValue?.[field.name] ??
      field.default ??
      (field.type === 'checkbox' ? false : '')
  }
}

// A checkboxarray holds the legacy CSV string (sys_user.modules), not an
// array: the value stays exactly what the API stores, so no conversion is
// needed on either side of the request.
function csvHas(field: FormField, option: string): boolean {
  return String(values[field.name] ?? '')
    .split(',')
    .includes(option)
}

// toggleCsv rewrites the CSV in the field's own option order, so the stored
// value does not depend on the order the boxes were clicked in.
function toggleCsv(field: FormField, option: string, on: boolean) {
  const current = new Set(
    String(values[field.name] ?? '')
      .split(',')
      .filter(Boolean),
  )
  if (on) current.add(option)
  else current.delete(option)
  values[field.name] = (field.options ?? [])
    .map((o) => o.value)
    .filter((v) => current.has(v))
    .join(',')
}

// fieldLabel resolves a field name to its display label for the error
// summary (falls back to the raw name for fixed/server-only fields).
function fieldLabel(name: string): string {
  for (const tab of props.metadata.tabs) {
    const field = tab.fields.find((f) => f.name === name)
    if (field?.label) return field.label
  }
  return name
}

const errorList = () => Object.entries(props.errors ?? {}).filter(([, msgs]) => msgs.length > 0)

// sections splits a tab at its legends so a collapsible legend can fold the
// fields that follow it (legacy collapse fieldsets).
function sections(tab: FormTab): Section[] {
  const out: Section[] = [{ collapsible: false, fields: [] }]
  for (const field of tab.fields) {
    if (field.type === 'legend') {
      out.push({ legend: field, collapsible: field.collapsible === true, fields: [] })
    } else {
      out[out.length - 1].fields.push(field)
    }
  }
  return out.filter((s) => s.legend || s.fields.length)
}

watch(
  values,
  (v) => emit('values-change', { ...v }),
  { deep: true },
)

// When server validation errors arrive, stay on (or jump to) the first tab
// holding an offending field so the inline message is visible.
watch(
  () => props.errors,
  (errors) => {
    const bad = new Set(
      Object.entries(errors ?? {})
        .filter(([, msgs]) => msgs.length > 0)
        .map(([field]) => field),
    )
    if (bad.size === 0) return
    const hasBadField = (name: string) =>
      props.metadata.tabs.find((tab) => tab.name === name)?.fields.some((f) => bad.has(f.name))
    if (hasBadField(activeTab.value)) return
    const offending = props.metadata.tabs.find((tab) => tab.fields.some((f) => bad.has(f.name)))
    if (offending) activeTab.value = offending.name
  },
  { immediate: true },
)
</script>

<template>
  <form
    class="border border-border bg-surface"
    data-test="tabbed-form"
    @submit.prevent="emit('save', { ...values })"
  >
    <!-- Flat tabs -->
    <div role="tablist" class="flex border-b border-border bg-bg">
      <button
        v-for="tab in metadata.tabs"
        :key="tab.name"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.name"
        :aria-controls="`tabpanel-${tab.name}`"
        class="-mb-px border-r border-border px-4 py-2 text-sm font-bold transition-colors duration-150"
        :class="
          activeTab === tab.name
            ? 'border-b border-b-surface bg-surface text-text'
            : 'border-b border-b-border text-text-muted hover:bg-info'
        "
        @click="activeTab = tab.name"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- Error summary (alert-danger composition) -->
    <UiAlert
      v-if="errorList().length"
      variant="danger"
      class="m-4"
      :messages="errorList().map(([field, msgs]) => `${fieldLabel(field)}: ${msgs.join(', ')}`)"
    />

    <!-- Fields of the active tab -->
    <div class="px-4 py-4">
      <template v-for="tab in metadata.tabs" :key="tab.name">
        <div
          v-show="activeTab === tab.name"
          :id="`tabpanel-${tab.name}`"
          role="tabpanel"
          class="mx-auto max-w-xl space-y-2"
        >
          <!-- One block per legend; a collapsible legend folds into <details> -->
          <component
            :is="section.collapsible ? 'details' : 'div'"
            v-for="(section, si) in sections(tab)"
            :key="si"
            class="space-y-2"
          >
            <summary
              v-if="section.collapsible"
              class="cursor-pointer border-b border-border pb-1 text-sm font-bold text-text"
            >
              {{ section.legend?.label }}
            </summary>
            <!-- Fieldset legend as a sub-heading (original trait) -->
            <p
              v-else-if="section.legend"
              class="border-b border-border pb-1 text-sm font-bold text-text"
            >
              {{ section.legend.label }}
            </p>
            <template v-for="field in section.fields" :key="field.name">
            <div class="flex items-start gap-3">
            <label
              :for="`field-${field.name}`"
              class="w-44 shrink-0 pt-1 text-right text-sm font-semibold after:content-[':']"
            >
              {{ field.label }}
            </label>
            <div class="flex-1">
              <input
                v-if="field.type === 'text' || field.type === 'password'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                :type="field.type"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <textarea
                v-else-if="field.type === 'textarea'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                rows="4"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              />
              <select
                v-else-if="field.type === 'select'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as string"
                :disabled="field.readonly"
                class="w-full border border-border bg-surface px-2 py-1 text-sm outline-none focus:border-link disabled:bg-bg disabled:text-text/60"
                :class="{ 'border-danger-border': errors?.[field.name]?.length }"
              >
                <option v-for="opt in field.options" :key="opt.value" :value="opt.value">
                  {{ opt.label }}
                </option>
              </select>
              <input
                v-else-if="field.type === 'checkbox'"
                :id="`field-${field.name}`"
                v-model="values[field.name] as boolean"
                type="checkbox"
                :disabled="field.readonly"
                class="mt-1"
              />
              <div
                v-else-if="field.type === 'checkboxarray'"
                :id="`field-${field.name}`"
                class="flex flex-wrap gap-x-4 gap-y-1 pt-1"
              >
                <label
                  v-for="opt in field.options"
                  :key="opt.value"
                  class="flex items-center gap-1 text-sm"
                >
                  <input
                    type="checkbox"
                    :name="field.name"
                    :value="opt.value"
                    :checked="csvHas(field, opt.value)"
                    :disabled="field.readonly"
                    @change="toggleCsv(field, opt.value, ($event.target as HTMLInputElement).checked)"
                  />
                  {{ opt.label }}
                </label>
              </div>
              <p
                v-if="errors?.[field.name]?.length"
                class="mt-1 text-xs text-danger-text"
              >
                {{ errors[field.name].join(', ') }}
              </p>
            </div>
            </div>
            </template>
          </component>
        </div>
      </template>
    </div>

    <!-- Form-scoped extras (e.g. the DKIM generate panel) above the actions -->
    <slot name="extra" />

    <!-- Save / Cancel -->
    <div class="flex justify-end gap-2 border-t border-border bg-bg px-4 py-2">
      <button
        type="button"
        data-test="form-cancel"
        class="btn btn-default px-8"
        :disabled="saving"
        @click="emit('cancel')"
      >
        {{ t('form.cancel') }}
      </button>
      <button type="submit" data-test="form-save" class="btn btn-success px-8" :disabled="saving">
        {{ t('form.save') }}
      </button>
    </div>
  </form>
</template>
