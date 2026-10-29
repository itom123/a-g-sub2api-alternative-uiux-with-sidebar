import { describe, expect, it } from 'vitest'
import { iconPaths } from '../iconPaths'

type Point = [number, number]

function endpoints(path: string): Point[] {
  const tokens = path.match(/[MLHVAZ]|-?\d+(?:\.\d+)?/g) ?? []
  const points: Point[] = []
  let current: Point = [0, 0]
  let command = ''
  let index = 0

  while (index < tokens.length) {
    if (/^[MLHVAZ]$/.test(tokens[index])) command = tokens[index++]
    if (command === 'Z') continue

    if (command === 'M' || command === 'L') {
      current = [Number(tokens[index++]), Number(tokens[index++])]
    } else if (command === 'H') {
      current = [Number(tokens[index++]), current[1]]
    } else if (command === 'V') {
      current = [current[0], Number(tokens[index++])]
    } else if (command === 'A') {
      index += 5
      current = [Number(tokens[index++]), Number(tokens[index++])]
    } else {
      throw new Error(`Unsupported SVG command in ${path}`)
    }

    points.push(current)
  }

  return points
}

describe('canonical icon paths', () => {
  const entries = Object.entries(iconPaths)

  it('keeps every exported glyph unique', () => {
    const paths = entries.map(([, path]) => path)
    expect(new Set(paths).size).toBe(paths.length)
  })

  it('removes legacy duplicate entries and keeps data glyphs distinct', () => {
    expect('chart' in iconPaths).toBe(false)
    expect('chatBubble' in iconPaths).toBe(false)
    expect(new Set([iconPaths.chartBar, iconPaths.navUsage, iconPaths.signal]).size).toBe(3)
  })

  it('keeps all path endpoints inside the 3px square-cap safety zone', () => {
    for (const [name, path] of entries) {
      for (const [x, y] of endpoints(path)) {
        expect(x, `${name}: x=${x}`).toBeGreaterThanOrEqual(3.5)
        expect(x, `${name}: x=${x}`).toBeLessThanOrEqual(20.5)
        expect(y, `${name}: y=${y}`).toBeGreaterThanOrEqual(3.5)
        expect(y, `${name}: y=${y}`).toBeLessThanOrEqual(20.5)
      }
    }
  })

  it('keeps the key, shield, and navigation semantics distinct at small sizes', () => {
    expect(iconPaths.key).toContain('M16 4A4 4')
    expect(iconPaths.shield).toContain('M12 4L18.5 6.5')
    expect(iconPaths.chartBar).toContain('M5 19H19')
    expect(iconPaths.navUsage).toContain('A8 8')
    expect(iconPaths.railway).toContain('M4 6H10V10H14V14H20')
    expect(iconPaths.database).toContain('L16 9H8')
    expect(iconPaths.moon).toContain('M16 4H13V6')
  })
})
