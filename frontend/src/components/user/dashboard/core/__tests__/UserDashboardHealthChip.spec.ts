import { mount } from '@vue/test-utils'
import { describe, expect, it, beforeEach, vi } from 'vitest'
import UserDashboardHealthChip from '../UserDashboardHealthChip.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('UserDashboardHealthChip', () => { beforeEach(() => vi.clearAllMocks()); it.each(['awaiting_data', 'all_good', 'attention'] as const)('renders text and non-color icon for %s', (health) => { const w = mount(UserDashboardHealthChip, { props: { health } }); expect(w.get('[data-testid=health-chip]').text()).toContain(`accountInsights.health.${health === 'awaiting_data' ? 'awaitingData' : health === 'all_good' ? 'allGood' : 'attention'}`); expect(w.get('[data-testid=health-chip] span').text()).toBeTruthy() }) })
