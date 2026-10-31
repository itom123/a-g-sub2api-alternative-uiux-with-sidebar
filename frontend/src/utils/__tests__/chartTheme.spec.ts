import { afterEach, describe, expect, it, vi } from 'vitest'
import { Chart, type Plugin } from 'chart.js'
import { applyChartTheme, installChartTheme } from '../chartTheme'

afterEach(() => {
  document.documentElement.removeAttribute('style')
  vi.restoreAllMocks()
})

describe('chart theme', () => {
  it('keeps a single observed point visible without inventing a trend', () => {
    const chart = { config: { type: 'line', options: {} }, data: { datasets: [{ data: [2.5] }] } } as unknown as Chart
    applyChartTheme(chart)
    expect(chart.data.datasets[0]).toHaveProperty('pointRadius', 3)
    expect(chart.data.datasets[0].data).toEqual([2.5])
  })

  it('gives all five token series different strokes without empty point markers', () => {
    const datasets = Array.from({ length: 5 }, (_, i) => ({ label: `series-${i}`, data: [i, i + 1] }))
    const chart = { config: { type: 'line', options: {} }, data: { datasets } } as unknown as Chart
    applyChartTheme(chart)
    expect(new Set(chart.data.datasets.map(d => JSON.stringify(d.borderDash))).size).toBe(5)
    expect(chart.data.datasets.every(d => 'pointRadius' in d && d.pointRadius === 0)).toBe(true)
    expect(chart.data.datasets.map(d => d.data)).toEqual(datasets.map(d => d.data))
  })

  it('inverts existing charts without changing measurements or callbacks', () => {
    const callback = (value: number) => `${value}ms`
    const datasets = [{ data: [1, 2], label: 'QPS' }, { data: [3, 4], label: 'TPS' }]
    const chart = { config: { type: 'line', options: { scales: { y: { ticks: { callback } } }, plugins: { tooltip: { callbacks: { label: callback } }, legend: { display: false } } } }, data: { datasets } } as unknown as Chart
    document.documentElement.style.setProperty('--ag-ink', '#000000')
    document.documentElement.style.setProperty('--ag-paper', '#ffffff')
    applyChartTheme(chart)
    expect(chart.data.datasets[0].borderColor).toBe('#000000')
    expect(chart.data.datasets[1]).toHaveProperty('borderDash', [6, 4])
    expect(chart.config.options?.plugins?.legend?.labels).toMatchObject({ usePointStyle: true, pointStyle: 'line' })
    document.documentElement.style.setProperty('--ag-ink', '#ffffff')
    document.documentElement.style.setProperty('--ag-paper', '#000000')
    applyChartTheme(chart)
    expect(chart.data.datasets[0].borderColor).toBe('#ffffff')
    expect(chart.data.datasets.map(d => d.data)).toEqual([[1, 2], [3, 4]])
    expect(chart.config.options?.scales?.y?.ticks?.callback).toBe(callback)
    expect(chart.config.options?.plugins?.tooltip?.callbacks?.label).toBe(callback)
    expect(chart.config.options?.plugins?.legend?.display).toBe(false)
    expect(chart.config.options?.plugins?.tooltip?.bodyColor).toBe('#000000')
    const register = vi.spyOn(Chart, 'register')
    vi.spyOn(MutationObserver.prototype, 'observe').mockReturnValue(undefined)
    installChartTheme()
    const plugin = register.mock.calls[0].find(p => 'id' in p && p.id === 'ag-monochrome') as Plugin
    const legendItems = [{ text: 'QPS', datasetIndex: 0 }, { text: 'TPS', datasetIndex: 1 }]
    Object.assign(chart, { legend: { legendItems }, ctx: { save: vi.fn(), fillRect: vi.fn(), restore: vi.fn() } })
    plugin.beforeDraw?.(chart, { cancelable: true }, {})
    expect(legendItems).toEqual([{ text: 'QPS', datasetIndex: 0, lineDash: [] }, { text: 'TPS', datasetIndex: 1, lineDash: [6, 4] }])
  })
})
