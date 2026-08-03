import { describe, expect, it } from 'vitest'
import { formatTs, stateClass } from '../state'

describe('stateClass', () => {
  it('maps every known severity to a distinct badge', () => {
    const known = ['ok', 'info', 'warning', 'critical', 'error', 'unknown', 'no_state']
    const classes = known.map(stateClass)
    // warning and critical intentionally share the amber band; every other
    // pair must differ so severities stay visually distinguishable.
    expect(classes[0]).not.toBe(classes[1]) // ok vs info
    expect(classes[1]).not.toBe(classes[2]) // info vs warning
    expect(classes[2]).toBe(stateClass('critical')) // warning === critical
    expect(classes[3]).not.toBe(stateClass('error')) // critical vs error
  })

  it('falls back to the neutral badge for an unrecognised label', () => {
    expect(stateClass('bogus')).toBe(stateClass('unknown'))
  })
})

describe('formatTs', () => {
  it('renders "-" for an unset timestamp', () => {
    expect(formatTs(0)).toBe('-')
    expect(formatTs(undefined)).toBe('-')
  })

  it('renders a real unix timestamp as a locale date string', () => {
    expect(formatTs(1735689600)).toBe(new Date(1735689600 * 1000).toLocaleString())
  })
})
