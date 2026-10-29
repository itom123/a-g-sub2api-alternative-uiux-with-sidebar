import { describe, expect, it } from 'vitest'
import { getWorkBuddyCapabilities, workBuddyEndpoint } from '../workbuddySetup'

describe('WorkBuddy setup guidance', () => {
  it('does not invent capabilities for unknown models or infer no tools from no parallel calls', () => {
    const unknown = getWorkBuddyCapabilities({ slug: 'unknown', supports_parallel_tool_calls: false })
    expect(unknown.toolCalling).toBeUndefined()
    expect(unknown.reasoningOnly).toBeUndefined()
    expect(unknown.allowDisablingReasoning).toBeUndefined()
    expect(unknown.imageInput).toBeUndefined()
    expect(getWorkBuddyCapabilities().efforts).toEqual([])
  })
  it('uses exact reported capabilities and only WorkBuddy effort choices', () => {
    const capabilities = getWorkBuddyCapabilities({ slug: 'example', input_modalities: ['text', 'image'], supports_parallel_tool_calls: true, supported_reasoning_levels: [{ effort: 'low' }, { effort: 'xhigh' }, { effort: 'max' }, { effort: 'none' }, { effort: 'unknown' }] })
    expect(capabilities).toMatchObject({ toolCalling: true, imageInput: true, reasoning: true, efforts: ['low', 'xhigh', 'max'] })
    expect(getWorkBuddyCapabilities({ slug: 'example', input_modalities: ['text'], supports_tool_calls: false, supports_parallel_tool_calls: true }).toolCalling).toBe(false)
  })
  it('uses a complete standard endpoint without duplicating v1', () => {
    expect(workBuddyEndpoint('https://example.com/v1/')).toBe('https://example.com/v1/chat/completions')
    expect(workBuddyEndpoint('https://example.com/')).toBe('https://example.com/v1/chat/completions')
  })
})

import { effectiveWorkBuddyEffort, parseWorkBuddyModelIds } from '../workbuddySetup'

describe('group-specific WorkBuddy guidance', () => {
  const metadata = { slug: 'gpt-6-sol', default_reasoning_level: 'max', supported_reasoning_levels: ['low', 'medium', 'high', 'xhigh', 'max'].map(effort => ({ effort })) }
  it('removes mapped and capped efforts and recommends the effective default', () => {
    const middle = getWorkBuddyCapabilities(metadata, { max_reasoning_effort: 'xhigh', reasoning_effort_mappings: [{ from: 'max', to: 'xhigh' }] })
    expect(middle.efforts).toEqual(['low', 'medium', 'high', 'xhigh'])
    expect(middle.defaultEffort).toBe('xhigh')
    expect(middle.changes).toContainEqual({ from: 'max', to: 'xhigh' })
    expect(getWorkBuddyCapabilities(metadata, { max_reasoning_effort: 'high' }).efforts).toEqual(['low', 'medium', 'high'])
    expect(getWorkBuddyCapabilities(metadata).efforts).toHaveLength(5)
  })
  it('reflects model-scoped denial and gateway mapping precedence', () => {
    const group = { max_reasoning_effort: 'high', max_reasoning_effort_over_limit: 'deny', reasoning_effort_mappings: [
      { from: 'max', to: 'low' },
      { from: 'max', to: 'medium', model: 'gpt-', match_type: 'prefix' as const },
      { from: 'max', to: 'deny', model: 'gpt-6-sol', match_type: 'exact' as const }
    ] }
    expect(effectiveWorkBuddyEffort('max', 'gpt-6-sol', group)).toBe('deny')
    expect(effectiveWorkBuddyEffort('max', 'gpt-6-astra', group)).toBe('medium')
    expect(effectiveWorkBuddyEffort('max', 'other', group)).toBe('low')
    expect(effectiveWorkBuddyEffort('xhigh', 'other', group)).toBe('deny')
    expect(getWorkBuddyCapabilities(metadata, group).efforts).not.toContain('max')
  })
  it('shows applicable policy mappings without inventing model support', () => {
    const caps = getWorkBuddyCapabilities(undefined, { reasoning_effort_mappings: [{ from: 'none', to: 'low' }] }, 'unknown')
    expect(caps.allowDisablingReasoning).toBe(false)
    expect(caps.changes).toEqual([{ from: 'none', to: 'low' }])
    expect(caps.reasoning).toBeUndefined()
  })
  it('handles ordinary, manifest, and wrapped manifest discovery formats', () => {
    expect(parseWorkBuddyModelIds({ data: [{ id: 'a-b' }, { id: 'a-b' }] })).toEqual(['a-b'])
    expect(parseWorkBuddyModelIds({ models: [{ slug: 'a-b' }] })).toEqual(['a-b'])
    expect(parseWorkBuddyModelIds({ data: { models: [{ slug: 'a-b' }] } })).toEqual(['a-b'])
    expect(parseWorkBuddyModelIds({ error: 'blocked' })).toEqual([])
  })
})
