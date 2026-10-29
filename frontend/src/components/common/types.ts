/**
 * Common component types
 */

export interface Column {
  key: string
  label: string
  fullLabel?: string
  width?: number
  minWidth?: number
  mobileLayout?: 'stacked'
  sortable?: boolean
  class?: string
  formatter?: (value: any, row: any) => string
}
