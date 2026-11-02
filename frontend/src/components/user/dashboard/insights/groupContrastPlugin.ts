import type { Plugin } from 'chart.js'

const isGroupChart = (chart: { canvas?: HTMLCanvasElement | null }): boolean =>
  Boolean(chart.canvas?.closest('.account-group-distribution'))

function hatchPattern(ink: string, paper: string): CanvasPattern | string {
  if (typeof document === 'undefined') return ink
  const tile = document.createElement('canvas')
  tile.width = tile.height = 8
  const context = tile.getContext('2d')
  if (!context) return ink
  context.fillStyle = paper
  context.fillRect(0, 0, tile.width, tile.height)
  context.strokeStyle = ink
  context.lineWidth = 1
  for (const offset of [-8, 0, 8]) {
    context.beginPath()
    context.moveTo(offset, 8)
    context.lineTo(offset + 8, 0)
    context.stroke()
  }
  return context.createPattern(tile, 'repeat') || ink
}

/** Keep the account group donut paper/ink-only while preserving segment contrast. */
export const accountGroupContrastPlugin: Plugin = {
  id: 'account-group-monochrome-contrast',
  beforeInit(chart) {
    if (isGroupChart(chart)) chart.options.animation = false
  },
  beforeDatasetUpdate(chart) {
    if (!isGroupChart(chart)) return
    const dataset = chart.data.datasets[0]
    if (!dataset) return
    const count = Array.isArray(dataset.data) ? dataset.data.length : 0
    const root = typeof document !== 'undefined' ? document.documentElement : null
    const styles = root ? getComputedStyle(root) : null
    const ink = styles?.getPropertyValue('--ag-ink').trim() || '#000000'
    const paper = styles?.getPropertyValue('--ag-paper').trim() || '#ffffff'
    const dark = Boolean(root?.classList.contains('dark'))
    dataset.backgroundColor = Array.from({ length: count }, (_, index) => index % 2 === 1 ? hatchPattern(ink, paper) : ink)
    dataset.hoverBackgroundColor = dataset.backgroundColor
    dataset.borderColor = Array.from({ length: count }, () => dark ? ink : paper)
    dataset.borderWidth = dark ? 2 : 1
  },
}
