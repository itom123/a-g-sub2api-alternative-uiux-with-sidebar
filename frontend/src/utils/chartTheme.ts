import { Chart, Tooltip, type Plugin, type CartesianScaleOptions, type TooltipModel } from 'chart.js'

const dashes = [[], [6, 4], [2, 3], [10, 3, 2, 3], [10, 3, 2, 3, 2, 3], [14, 4], [4, 3, 4, 8], [1, 3]]
const tooltipElements = new Map<Chart, HTMLDivElement>()

/** DOM tooltips escape the canvas bounds and keep long labels readable. */
function externalTooltip({ chart, tooltip }: { chart: Chart; tooltip: TooltipModel<'line'> }) {
  let element = tooltipElements.get(chart)
  if (!tooltip.opacity) {
    if (element) element.hidden = true
    return
  }
  if (!element) {
    element = document.createElement('div')
    element.className = 'ag-chart-tooltip'
    element.setAttribute('role', 'tooltip')
    document.body.appendChild(element)
    tooltipElements.set(chart, element)
  }
  element.replaceChildren()
  const title = document.createElement('strong')
  title.textContent = tooltip.title.join(' · ')
  element.appendChild(title)
  for (const line of [...tooltip.beforeBody, ...tooltip.body.flatMap(b => [...b.before, ...b.lines, ...b.after]), ...tooltip.afterBody, ...tooltip.footer]) {
    const paragraph = document.createElement('p')
    paragraph.textContent = line
    element.appendChild(paragraph)
  }
  element.hidden = false
  element.style.maxWidth = `${Math.min(440, window.innerWidth - 24)}px`
  const canvas = chart.canvas.getBoundingClientRect()
  const bounds = element.getBoundingClientRect()
  const below = canvas.bottom + 8
  // Prefer outside the chart; near the pointer is the fallback on tall canvases.
  const preferredTop = below + bounds.height <= window.innerHeight - 12
    ? below : canvas.top - bounds.height - 8 >= 12
      ? canvas.top - bounds.height - 8 : canvas.top + tooltip.caretY + 16
  element.style.left = `${Math.max(12, Math.min(canvas.left + tooltip.caretX, window.innerWidth - bounds.width - 12))}px`
  element.style.top = `${Math.max(12, Math.min(preferredTop, window.innerHeight - bounds.height - 12))}px`
}

/** Pure paper/ink patterns distinguish areas without changing their values. */
function pattern(ink: string, paper: string, index: number): CanvasPattern | string {
  const tile = document.createElement('canvas')
  tile.width = tile.height = 8
  const ctx = tile.getContext('2d')
  if (!ctx) return ink
  ctx.fillStyle = index % 4 === 0 ? ink : paper
  ctx.fillRect(0, 0, 8, 8)
  ctx.strokeStyle = ink
  ctx.fillStyle = ink
  ctx.lineWidth = 1
  if (index % 4 === 1) {
    for (const offset of [-8, 0, 8]) {
      ctx.beginPath(); ctx.moveTo(offset, 8); ctx.lineTo(offset + 8, 0); ctx.stroke()
    }
  } else if (index % 4 === 2) {
    ctx.beginPath(); ctx.arc(4, 4, 1, 0, Math.PI * 2); ctx.fill()
  } else if (index % 4 === 3) {
    ctx.beginPath(); ctx.moveTo(4, 0); ctx.lineTo(4, 8); ctx.moveTo(0, 4); ctx.lineTo(8, 4); ctx.stroke()
  }
  return ctx.createPattern(tile, 'repeat') || ink
}

export function applyChartTheme(chart: Chart) {
  const styles = getComputedStyle(document.documentElement)
  const ink = styles.getPropertyValue('--ag-ink').trim() || '#000000'
  const paper = styles.getPropertyValue('--ag-paper').trim() || '#ffffff'
  const family = styles.getPropertyValue('--ag-font').trim() || 'Arial, sans-serif'
  const type = 'type' in chart.config ? chart.config.type : undefined
  const options = chart.config.options ||= {}
  options.color = ink
  options.font = { ...options.font, family, size: 12 }
  for (const scale of Object.values(options.scales || {})) {
    if (!scale) continue
    scale.ticks = { ...scale.ticks, color: ink, font: { family, size: 12 } }
    scale.grid = { ...scale.grid, color: ink, lineWidth: .25 }
    if (scale.type !== 'radialLinear') {
      const axis = scale as unknown as CartesianScaleOptions
      Object.assign(scale, { border: { ...axis.border, color: ink, dash: [2, 4] }, title: { ...axis.title, color: ink, font: { family, size: 12 } } })
    }
  }
  const plugins = options.plugins ||= {}
  {
    const legend = plugins.legend ||= {}
    legend.labels = { ...legend.labels, color: ink, font: { family, size: 12 }, boxWidth: 20, boxHeight: 8 }
    if (type === 'line') Object.assign(legend.labels, { usePointStyle: true, pointStyle: 'line', pointStyleWidth: 64 })
  }
  {
    plugins.tooltip = { ...plugins.tooltip, enabled: false, external: externalTooltip, backgroundColor: ink, titleColor: paper, bodyColor: paper,
      footerColor: paper, borderColor: paper, borderWidth: 1, cornerRadius: 0,
      titleFont: { family, size: 13 }, bodyFont: { family, size: 12 }, footerFont: { family, size: 12 } }
  }
  if (plugins.title) plugins.title = { ...plugins.title, color: ink, font: { family, size: 14 } }
  chart.data.datasets.forEach((dataset, index) => {
    const kind = dataset.type || type
    // Mixed chart dataset styling; leave callbacks, labels and data untouched.
    const style = dataset as unknown as Record<string, unknown>
    style.borderColor = ink
    style.borderWidth = kind === 'line' ? 1.5 : 1
    style.hoverBorderColor = ink
    if (kind === 'line') {
      style.backgroundColor = paper
      style.borderDash = dashes[index % dashes.length]
      style.pointBackgroundColor = paper
      style.pointBorderColor = ink
      style.pointStyle = ['circle', 'rect', 'triangle', 'cross'][index % 4]
      style.pointRadius = dataset.data.length === 1 ? 3 : 0
      style.pointHoverRadius = 4
      style.fill = false
      style.tension = .15
    } else {
      const perItem = kind === 'doughnut' || kind === 'pie' || kind === 'polarArea'
      style.backgroundColor = perItem ? dataset.data.map((_, i) => pattern(ink, paper, i)) : pattern(ink, paper, index)
      style.hoverBackgroundColor = style.backgroundColor
      style.borderRadius = 0
      style.borderSkipped = false
    }
  })
}

const monochrome: Plugin = {
  id: 'ag-monochrome',
  beforeInit: applyChartTheme,
  beforeUpdate: applyChartTheme,
  afterDestroy(chart) {
    tooltipElements.get(chart)?.remove()
    tooltipElements.delete(chart)
  },
  beforeDraw(chart) {
    // Point-style legends otherwise read point options and lose the line dashes.
    if ('type' in chart.config && chart.config.type === 'line') {
      for (const item of chart.legend?.legendItems || []) item.lineDash = dashes[(item.datasetIndex || 0) % dashes.length]
    }
    // Keep exported PNGs as readable as the on-screen chart in either theme.
    const paper = getComputedStyle(document.documentElement).getPropertyValue('--ag-paper').trim() || '#ffffff'
    chart.ctx.save()
    chart.ctx.fillStyle = paper
    chart.ctx.fillRect(0, 0, chart.width, chart.height)
    chart.ctx.restore()
  }
}

export function installChartTheme() {
  Chart.register(Tooltip, monochrome)
  Chart.defaults.plugins.tooltip.enabled = false
  Chart.defaults.plugins.tooltip.external = externalTooltip
  window.addEventListener('scroll', () => {
    for (const element of tooltipElements.values()) element.hidden = true
  }, true)
  // Existing charts must redraw when the sidebar or public header changes theme.
  new MutationObserver(() => {
    for (const chart of Object.values(Chart.instances)) chart.update('none')
  }).observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
}
