<script setup lang="ts">
// Filled line chart for the dashboard System metrics dashlet (port of the
// legacy dashlets/templates/metrics.htm Chart.js line charts: teal stroke,
// filled area, hidden x-axis, y starting at zero).
import { computed } from 'vue'
import { Line } from 'vue-chartjs'
import 'chart.js/auto'
import { useI18n } from '../i18n'

const props = defineProps<{
  /** label is the series name shown as the chart legend. */
  label: string
  /** values is the rolling series, oldest first (max 15 points). */
  values: number[]
  /** times are the matching HH:MM stamps, used as point tooltips. */
  times?: string[]
}>()

const { t } = useI18n()

const chartData = computed(() => ({
  labels: props.values.map((_, i) => props.times?.[i] ?? ''),
  datasets: [
    {
      label: props.label,
      data: props.values,
      borderColor: 'rgb(75, 192, 192)',
      backgroundColor: 'rgba(75, 192, 192, 0.2)',
      borderWidth: 1,
      pointRadius: 0,
      pointHitRadius: 8,
      fill: true,
      tension: 0.1,
    },
  ],
}))

// Legacy options: no legend (the figcaption carries the name), no visible
// x-axis, y anchored at zero.
const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false as const,
  plugins: { legend: { display: false } },
  scales: {
    x: { display: false },
    y: { beginAtZero: true },
  },
}
</script>

<template>
  <figure class="border border-border bg-surface p-2" data-test="metric-chart">
    <figcaption class="mb-1 text-center text-xs font-bold text-text">{{ label }}</figcaption>
    <div class="h-24">
      <Line v-if="values.length" :data="chartData" :options="chartOptions" />
      <p v-else class="flex h-full items-center justify-center text-xs text-text-muted">
        {{ t('dashboard.metrics.no_data') }}
      </p>
    </div>
  </figure>
</template>
