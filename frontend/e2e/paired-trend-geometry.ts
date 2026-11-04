import { expect, type Page } from '@playwright/test'

export async function readPairedGeometry(page: Page) {
  return page.getByTestId('account-trends').locator('canvas').evaluateAll(canvases => canvases.map(canvas => {
    const chart = (canvas as HTMLCanvasElement & { accountTrendChart?: any }).accountTrendChart
    if (!chart) throw new Error('Native Chart.js instance is not ready')
    const bounds = canvas.getBoundingClientRect()
    const scaleX = bounds.width / chart.width
    const scaleY = bounds.height / chart.height
    const area = chart.chartArea
    return {
      left: bounds.left + area.left * scaleX,
      right: bounds.left + area.right * scaleX,
      topInset: area.top * scaleY,
      bottomInset: (chart.height - area.bottom) * scaleY,
      height: (area.bottom - area.top) * scaleY,
      buckets: chart.data.labels.map((label: string, index: number) => ({ label, x: bounds.left + chart.scales.x.getPixelForValue(index) * scaleX })),
      ticks: chart.scales.x.ticks.map((tick: any) => ({ value: tick.value, x: chart.scales.x.getPixelForValue(tick.value) * scaleX })),
      yTicks: chart.scales.y.ticks.map((tick: any) => (chart.scales.y.getPixelForValue(tick.value) - area.top) * scaleY),
    }
  }))
}

export async function assertPairedGeometry(page: Page) {
  await expect.poll(async () => {
    try {
      const [a, b] = await readPairedGeometry(page)
      if (!a || !b || a.right - a.left < 100 || b.right - b.left < 100 || a.height < 100 || b.height < 100 || a.buckets.length !== b.buckets.length) return false
      return ['left', 'right', 'topInset', 'bottomInset', 'height'].every(key => Math.abs(a[key as keyof typeof a] as number - (b[key as keyof typeof b] as number)) <= 1)
    } catch { return false }
  }).toBe(true)
  const [a, b] = await readPairedGeometry(page)
  expect(a.ticks.map(tick => tick.value)).toEqual(b.ticks.map(tick => tick.value))
  expect(a.yTicks).toHaveLength(b.yTicks.length)
  a.yTicks.forEach((value, index) => expect(Math.abs(value - b.yTicks[index])).toBeLessThanOrEqual(1))
  a.buckets.forEach((bucket, index) => {
    expect(bucket.label).toBe(b.buckets[index].label)
    expect(Math.abs(bucket.x - b.buckets[index].x)).toBeLessThanOrEqual(1)
  })
  return { cost: a, tokens: b }
}
