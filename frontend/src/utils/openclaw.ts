export const OPENCLAW_BILLING_GROUP_NAME = 'openai-龙虾'

export function isManagedOpenClawGroup(name?: string): boolean {
  const normalized = name?.trim().toLowerCase()
  return normalized === OPENCLAW_BILLING_GROUP_NAME || normalized === 'claw'
}
