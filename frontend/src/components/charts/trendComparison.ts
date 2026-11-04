import type { Chart, ChartOptions, Plugin, Scale } from 'chart.js'

export interface TrendComparisonLayout {
  labels: string[]
  tickIndices: number[]
  gutter: number
  maximum: number
}

export interface ComparisonCanvas extends HTMLCanvasElement {
  /** Native instance for bounded layout inspection; removed when the chart is destroyed. */
  accountTrendChart?: Chart<'line'>
}

export const comparisonDashes = ['', '6 4', '2 3', '10 3 2 3']

export function comparisonTickIndices(length: number, width: number): number[] {
  const count = Math.min(length, Math.max(2, Math.min(8, Math.floor((width - 100) / 72))))
  if (length <= 1) return length ? [0] : []
  return Array.from(new Set(Array.from({ length: count }, (_, index) => Math.round(index * (length - 1) / (count - 1)))))
}

export function comparisonDateLabel(label: string): string {
  // Backend bucket keys already carry the requested timezone. Do not parse date-only keys as UTC.
  const hour = label.match(/(?:T|\s)(\d{2}:\d{2})/)
  return hour ? hour[1] : label.replace(/^\d{4}-/, '')
}

export function comparisonMaximum(values: number[]): number {
  const maximum = Math.max(0, ...values.filter(Number.isFinite))
  if (!maximum) return 1
  const step = maximum / 5
  const power = 10 ** Math.floor(Math.log10(step))
  return Math.ceil(step / power) * power * 5
}

export function comparisonScales(layout: TrendComparisonLayout, unit: string, formatValue: (value: number) => string): ChartOptions<'line'>['scales'] {
  return {
    x: {
      type: 'category', offset: false, min: 0, max: Math.max(0, layout.labels.length - 1),
      afterBuildTicks(axis: Scale) { axis.ticks = layout.tickIndices.map(value => ({ value })) },
      afterFit(axis: Scale) { axis.height = 32 },
      ticks: {
        autoSkip: false, minRotation: 0, maxRotation: 0, padding: 6,
        callback(value) { return comparisonDateLabel(layout.labels[Number(value)] || '') },
      },
    },
    y: {
      type: 'linear', min: 0, max: layout.maximum, beginAtZero: true,
      afterFit(axis: Scale) { axis.width = layout.gutter },
      title: { display: true, text: unit },
      ticks: { count: 6, autoSkip: false, padding: 6, callback: value => formatValue(Number(value)) },
    },
  }
}

export function createTrendComparison(onSelect: (index: number | null) => void) {
  const charts = new Set<Chart<'line'>>()
  const canvases = new WeakMap<Chart<'line'>, ComparisonCanvas>()
  let selected: number | null = null
  let hidden: number[] = []
  const suppressTooltip = (chart: Chart<'line'>) => {
    if (chart.options.plugins?.tooltip) {
      chart.options.plugins.tooltip.enabled = false
      chart.options.plugins.tooltip.external = () => {}
    }
  }
  const plugin: Plugin<'line'> = {
    id: 'account-paired-trends',
    beforeInit: suppressTooltip,
    beforeUpdate: suppressTooltip,
    afterInit(chart) {
      charts.add(chart)
      const canvas = chart.canvas as ComparisonCanvas
      canvases.set(chart, canvas)
      canvas.accountTrendChart = chart
    },
    afterUpdate(chart) {
      if (chart.data.datasets.length > 1) hidden.forEach(index => chart.setDatasetVisibility(index, false))
    },
    afterEvent(chart, { event, inChartArea }) {
      if (event.type === 'mouseout') { onSelect(null); return }
      if (!inChartArea || event.x == null || !chart.data.labels?.length) return
      const index = Number(chart.scales.x.getValueForPixel(event.x))
      if (Number.isFinite(index)) onSelect(Math.min(chart.data.labels.length - 1, Math.max(0, Math.round(index))))
    },
    afterDraw(chart) {
      if (selected === null || selected >= (chart.data.labels?.length || 0)) return
      const x = chart.scales.x.getPixelForValue(selected)
      const { ctx, chartArea } = chart
      ctx.save()
      ctx.strokeStyle = getComputedStyle(chart.canvas).color
      ctx.lineWidth = 1
      ctx.setLineDash([3, 3])
      ctx.beginPath(); ctx.moveTo(x, chartArea.top); ctx.lineTo(x, chartArea.bottom); ctx.stroke()
      ctx.restore()
    },
    afterDestroy(chart) {
      charts.delete(chart)
      const canvas = canvases.get(chart)
      if (canvas) delete canvas.accountTrendChart
      canvases.delete(chart)
    },
  }
  return {
    plugin,
    select(index: number | null) { selected = index; charts.forEach(chart => chart.draw()) },
    setHidden(indices: number[]) {
      hidden = [...indices]
      charts.forEach(chart => {
        if (chart.data.datasets.length <= 1) return
        chart.data.datasets.forEach((_, index) => chart.setDatasetVisibility(index, !hidden.includes(index)))
        chart.update('none')
      })
    },
  }
}
