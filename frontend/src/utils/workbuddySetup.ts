import type { Group, ReasoningEffortMapping } from '@/types'

export interface WorkBuddyModelMetadata {
  slug: string
  input_modalities?: string[]
  supported_reasoning_levels?: { effort: string }[]
  default_reasoning_level?: string
  supports_tool_calls?: boolean
  supports_parallel_tool_calls?: boolean
  supports_reasoning?: boolean
  reasoning_only?: boolean
  allow_disabling_reasoning?: boolean
}

const rankedEfforts = ['minimal', 'low', 'medium', 'high', 'xhigh', 'max']
export const workBuddyEffortLabels: Record<string, string> = { none: 'None', minimal: 'Minimal', low: 'Low', medium: 'Medium', high: 'High', xhigh: 'X-High', max: 'Max' }
const normalizeEffort = (value: string) => value.trim().toLowerCase().replace(/[-_ ]/g, '').replace('extrahigh', 'xhigh')
type Policy = Pick<Group, 'max_reasoning_effort' | 'max_reasoning_effort_over_limit' | 'reasoning_effort_mappings'>

// Match the gateway's exact > longest prefix/suffix > global precedence.
function selectMapping(mappings: ReasoningEffortMapping[], model: string, effort: string) {
  const id = model.trim().toLowerCase()
  return mappings.flatMap((mapping, index) => {
    if (normalizeEffort(mapping.from) !== effort) return []
    const pattern = (mapping.model || '').trim().toLowerCase()
    const type = pattern ? mapping.match_type || 'exact' : ''
    const matches = !type || (type === 'exact' ? id === pattern : type === 'prefix' ? id.startsWith(pattern) : id.endsWith(pattern))
    return matches ? [{ mapping, index, rank: type === 'exact' ? 3 : type ? 2 : 1, length: type ? pattern.length : 0 }] : []
  }).sort((a, b) => b.rank - a.rank || b.length - a.length || a.index - b.index)[0]?.mapping
}

export function effectiveWorkBuddyEffort(effort: string, model: string, group?: Policy): string {
  const requested = normalizeEffort(effort)
  const mapping = selectMapping(group?.reasoning_effort_mappings || [], model, requested)
  let effective = mapping ? normalizeEffort(mapping.to) : requested
  if (effective === 'deny') return effective
  const ceiling = normalizeEffort(group?.max_reasoning_effort || '')
  if (rankedEfforts.includes(ceiling) && rankedEfforts.indexOf(effective) > rankedEfforts.indexOf(ceiling)) {
    effective = group?.max_reasoning_effort_over_limit === 'deny' ? 'deny' : ceiling
  }
  return effective
}

export function getWorkBuddyCapabilities(model?: WorkBuddyModelMetadata, group?: Policy, modelId = model?.slug || '') {
  const nativeEfforts = [...new Set((model?.supported_reasoning_levels || []).map(level => normalizeEffort(level.effort)))]
    .filter(effort => ['low', 'medium', 'high', 'xhigh', 'max'].includes(effort))
  const policyEfforts = [...new Set([...nativeEfforts, ...(group?.reasoning_effort_mappings || []).map(mapping => normalizeEffort(mapping.from))])]
  const changes = policyEfforts.flatMap(from => {
    const to = effectiveWorkBuddyEffort(from, modelId, group)
    return to !== from ? [{ from, to }] : []
  })
  const efforts = nativeEfforts.filter(effort => effectiveWorkBuddyEffort(effort, modelId, group) === effort)
  const constrained = !!group?.max_reasoning_effort || !!changes.length
  const defaultEffort = constrained ? effectiveWorkBuddyEffort(model?.default_reasoning_level || '', modelId, group) : ''
  return {
    toolCalling: model?.supports_tool_calls ?? (model?.supports_parallel_tool_calls === true ? true : undefined),
    imageInput: model?.input_modalities ? model.input_modalities.includes('image') : undefined,
    reasoning: nativeEfforts.length && nativeEfforts.every(effort => ['deny', 'none'].includes(effectiveWorkBuddyEffort(effort, modelId, group))) ? false : model?.supports_reasoning ?? (nativeEfforts.length ? true : undefined),
    reasoningOnly: model?.reasoning_only,
    allowDisablingReasoning: effectiveWorkBuddyEffort('none', modelId, group) !== 'none' ? false : model?.allow_disabling_reasoning,
    efforts,
    nativeEfforts,
    changes,
    defaultEffort: efforts.includes(defaultEffort) ? defaultEffort : constrained ? efforts[0] : undefined
  }
}

export function parseWorkBuddyModelIds(payload: unknown): string[] {
  if (!payload || typeof payload !== 'object') return []
  const body = payload as { data?: unknown; models?: unknown }
  const rows = Array.isArray(body.data) ? body.data : Array.isArray(body.models) ? body.models : Array.isArray((body.data as { models?: unknown })?.models) ? (body.data as { models: unknown[] }).models : []
  return [...new Set(rows.flatMap((row: unknown) => {
    if (!row || typeof row !== 'object') return []
    const entry = row as { id?: unknown; slug?: unknown }
    const id = entry.id ?? entry.slug
    return typeof id === 'string' && id.trim() ? [id.trim()] : []
  }))]
}

export function workBuddyEndpoint(baseUrl: string): string {
  const base = baseUrl.trim().replace(/\/+$/, '')
  return `${base.endsWith('/v1') ? base : `${base}/v1`}/chat/completions`
}
