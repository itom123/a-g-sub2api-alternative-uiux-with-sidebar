import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Icon from '../Icon.vue'

type Point = [number, number]
type Segment = [Point, Point]
const names = ['auditLog', 'cog', 'navTenants', 'navOps', 'navGroups', 'sun'] as const

// These glyphs deliberately contain only absolute straight-line SVG commands.
function segments(path: string): Segment[] {
  const tokens = path.match(/[MLHVZ]|-?\d+(?:\.\d+)?/g)!
  const result: Segment[] = []
  let current: Point = [0, 0]
  let start: Point = current
  let command = ''
  let index = 0
  while (index < tokens.length) {
    if (/^[MLHVZ]$/.test(tokens[index])) command = tokens[index++]
    if (command === 'Z') {
      result.push([current, start])
      current = start
      continue
    }
    const next: Point = command === 'H' ? [Number(tokens[index++]), current[1]]
      : command === 'V' ? [current[0], Number(tokens[index++])]
        : [Number(tokens[index++]), Number(tokens[index++])]
    if (command === 'M') {
      start = next
      command = 'L'
    } else result.push([current, next])
    current = next
  }
  return result
}

function canonical(lines: Segment[], transform: (p: Point) => Point = p => p) {
  return lines.map(line => line.map(p => transform(p).map(n => n.toFixed(3)).join(',')).sort().join('|')).sort()
}

const paths = names.map(name => {
  const wrapper = mount(Icon, { props: { name } })
  const path = wrapper.get('path').attributes('d')
  wrapper.unmount()
  return [name, path] as const
})

describe('symmetric interface glyphs', () => {
  it.each(paths)('%s is symmetric, centered, inset, and made of straight lines', (_name, path) => {
    expect(path.replace(/[MLHVZ\d\s.,-]/g, '')).toBe('')
    const lines = segments(path)
    const expected = canonical(lines)
    expect(canonical(lines, ([x, y]) => [24 - x, 24 - y])).toEqual(expected)
    expect(canonical(lines, ([x, y]) => [x, 24 - y])).toEqual(expected)
    expect(canonical(lines, ([x, y]) => [24 - x, y])).toEqual(expected)
    for (const line of lines) for (const [x, y] of line) {
      expect(x).toBeGreaterThanOrEqual(3)
      expect(x).toBeLessThanOrEqual(21)
      expect(y).toBeGreaterThanOrEqual(3)
      expect(y).toBeLessThanOrEqual(21)
    }
  })

  it('gives each function a distinct shape', () => {
    const shapes = paths.map(([, path]) => JSON.stringify(canonical(segments(path))))
    expect(new Set(shapes).size).toBe(names.length)
  })
})
