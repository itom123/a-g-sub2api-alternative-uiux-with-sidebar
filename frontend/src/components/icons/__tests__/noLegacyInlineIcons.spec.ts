import { describe, expect, it } from 'vitest'

// Interface glyphs must come from <Icon> (iconPaths.ts) so every screen shows the
// current A.G. drawing. Inline <svg> is only allowed for brand marks, loading
// spinners, chart textures and the few data graphics listed below.
const sources = import.meta.glob('/src/**/*.vue', { query: '?raw', import: 'default', eager: true }) as Record<string, string>

const brandFiles = [
  'components/common/PlatformIcon.vue', 'components/common/ModelIcon.vue', 'components/common/GrokFreeIcon.vue',
  'components/user/monitor/ProviderIcon.vue', 'components/auth/GitHubMark.vue', 'components/auth/GoogleMark.vue',
  'components/auth/LinuxDoOAuthSection.vue', 'components/auth/DingTalkOAuthSection.vue', 'views/user/StripePaymentView.vue',
  'components/icons/Icon.vue',
]
const allowedMarkers = [
  'animate-spin', '<pattern', 'd="M0 6H24"', // spinners, chart textures and legend line samples
  'M12 2C6.477 2 2 6.477 2 12', // GitHub mark
  'M5 2L1.5 6.5', 'M5 8L1.5 3.5', 'M10 3l-4 5h8l-4-5z', // table sort indicators
  ':d="ringPath"', 'class="progress-ring"', // data gauges
]

function templateOf(source: string) {
  return source.replace(/<script\b[\s\S]*?<\/script>/g, '').replace(/<style\b[\s\S]*?<\/style>/g, '')
}

describe('interface icons', () => {
  it('uses the canonical Icon component instead of inline SVG glyphs', () => {
    const offenders: string[] = []
    for (const [path, source] of Object.entries(sources)) {
      if (brandFiles.some(file => path.endsWith(file))) continue
      for (const svg of templateOf(source).match(/<svg\b[\s\S]*?<\/svg>/g) ?? []) {
        if (allowedMarkers.some(marker => svg.includes(marker))) continue
        offenders.push(`${path}: ${svg.slice(0, 120).replace(/\s+/g, ' ')}`)
      }
    }
    expect(offenders).toEqual([])
  })
})
