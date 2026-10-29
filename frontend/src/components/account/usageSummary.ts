import type { ComputedRef, InjectionKey } from 'vue'

export interface UsageSummary {
  label: string
  caption: string
  percent: string
  utilization: number
}

// The table preview shares measurements with the mounted detail controls.
export const accountUsageSummaryKey: InjectionKey<Map<string, ComputedRef<UsageSummary>>> = Symbol('account-usage-summary')
