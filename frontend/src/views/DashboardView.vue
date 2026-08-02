<script setup lang="ts">
// Dashboard as ISPConfig3 dashlets: one card per enabled module
// (dashlet background, large module icon, title, full-width button).
import { CircleHelp } from 'lucide-vue-next'
import { moduleIcons } from '../icons'
import { modules } from '../modules'
import { useI18n } from '../i18n'

const { t } = useI18n()

// Every module except the dashboard itself becomes a dashlet.
const dashlets = modules.filter((mod) => mod.id !== 'dashboard')
</script>

<template>
  <div>
    <h1 class="mb-4 text-lg font-bold">{{ t('module.dashboard') }}</h1>

    <ul class="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-4">
      <li
        v-for="mod in dashlets"
        :key="mod.id"
        class="flex flex-col items-center gap-3 border border-border bg-dashlet p-5 shadow-sm"
        :data-test="`dashlet-${mod.id}`"
      >
        <component
          :is="moduleIcons[mod.id] ?? CircleHelp"
          :size="50"
          :stroke-width="1.25"
          class="text-text"
        />
        <span class="text-base font-bold">{{ t(`module.${mod.id}`) }}</span>
        <RouterLink :to="mod.path" class="btn btn-default w-full no-underline">
          {{ t('dashboard.open_module', { module: t(`module.${mod.id}`) }) }}
        </RouterLink>
      </li>
    </ul>
  </div>
</template>
