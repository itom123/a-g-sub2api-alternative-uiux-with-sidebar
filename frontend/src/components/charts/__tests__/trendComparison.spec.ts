import { describe, expect, it } from 'vitest'
import { comparisonDateLabel, comparisonMaximum, comparisonScales, comparisonTickIndices, createTrendComparison } from '../trendComparison'

describe('paired trend geometry contract', () => {
  it('cleans the captured canvas after Chart.js has nulled its own canvas reference', () => {
    const pair = createTrendComparison(() => {})
    const canvas = document.createElement('canvas') as HTMLCanvasElement & { accountTrendChart?: unknown }
    const chart = { canvas, options: { plugins: {} } } as any
    pair.plugin.afterInit?.(chart, {} as any, {} as any)
    expect(canvas.accountTrendChart).toBe(chart)
    chart.canvas = null
    expect(() => pair.plugin.afterDestroy?.(chart, {} as any, {} as any)).not.toThrow()
    expect(canvas.accountTrendChart).toBeUndefined()
  })

  it('selects common visible indices including both boundaries at every width', () => {
    expect(comparisonTickIndices(0, 320)).toEqual([])
    expect(comparisonTickIndices(1, 320)).toEqual([0])
    expect(comparisonTickIndices(7, 320)).toEqual([0, 3, 6])
    const thirty = comparisonTickIndices(30, 800)
    expect(thirty).toHaveLength(8)
    expect(thirty[0]).toBe(0)
    expect(thirty.at(-1)).toBe(29)
  })
  it('preserves timezone bucket strings without date parsing', () => {
    expect(comparisonDateLabel('2026-11-01')).toBe('11-01')
    expect(comparisonDateLabel('2026-11-01 03:00')).toBe('03:00')
    expect(comparisonDateLabel('2026-11-01T23:00:00+08:00')).toBe('23:00')
  })
  it('uses positive independent maxima and six equally spaced y ticks', () => {
    expect(comparisonMaximum([0, NaN])).toBe(1)
    expect(comparisonMaximum([100, 450])).toBe(450)
    const scales = comparisonScales({ labels: ['a', 'b'], tickIndices: [0, 1], gutter: 80, maximum: 500 }, 'CNY', String) as any
    const y = { width: 23 }
    const x = { height: 45, ticks: [] }
    scales.y.afterFit(y); scales.x.afterFit(x); scales.x.afterBuildTicks(x)
    expect(y.width).toBe(80)
    expect(x.height).toBe(32)
    expect(x.ticks).toEqual([{ value: 0 }, { value: 1 }])
    expect(scales.y.ticks.count).toBe(6)
    expect(scales.y.title.text).toBe('CNY')
  })
})
