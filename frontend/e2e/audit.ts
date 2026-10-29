import { expect } from '@playwright/test'
import type { Page, TestInfo } from '@playwright/test'
import axe from 'axe-core'

export async function audit(page: Page, info: TestInfo, name = 'page') {
  await page.evaluate(() => document.fonts.ready)
  // Vue starts enter transitions across two frames; wait for those before measuring.
  await page.evaluate(async () => {
    const frame = () => new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
    await frame()
    await frame()
    await Promise.all(document.getAnimations()
      .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity && Number(animation.effect?.getTiming().duration) < 1000)
      .map(animation => animation.finished.catch(() => {})))
  })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(1)
  await page.addScriptTag({ content: axe.source })
  const contrast = await page.evaluate(async () => {
    const result = await window.axe.run(document, { runOnly: ['color-contrast'] })
    return result.violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ target: n.target, summary: n.failureSummary })) }))
  })
  // Capture evidence even when the contrast assertion fails.
  await page.screenshot({ path: info.outputPath(`${name}.png`), fullPage: true, scale: 'css' })
  await info.attach(name, { path: info.outputPath(`${name}.png`), contentType: 'image/png' })
  expect(contrast, 'Text contrast').toEqual([])
}
