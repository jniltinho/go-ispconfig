// Soft route-loader timing: show delay (~100ms) and min-visible (~200ms).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useUiStore } from '../ui'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('ui store routeLoading', () => {
  it('does not flash the loader for navigations faster than the show delay', () => {
    const ui = useUiStore()
    ui.startRouteLoading()
    expect(ui.routeLoading).toBe(false)

    vi.advanceTimersByTime(50)
    ui.stopRouteLoading()
    vi.advanceTimersByTime(200)

    expect(ui.routeLoading).toBe(false)
  })

  it('shows the loader after the show delay', () => {
    const ui = useUiStore()
    ui.startRouteLoading()
    expect(ui.routeLoading).toBe(false)

    vi.advanceTimersByTime(100)
    expect(ui.routeLoading).toBe(true)
  })

  it('keeps the loader visible for at least the min-visible window', () => {
    const ui = useUiStore()
    ui.startRouteLoading()
    vi.advanceTimersByTime(100)
    expect(ui.routeLoading).toBe(true)

    // Stop immediately after show — still within min-visible (200ms).
    ui.stopRouteLoading()
    vi.advanceTimersByTime(100)
    expect(ui.routeLoading).toBe(true)

    vi.advanceTimersByTime(100)
    expect(ui.routeLoading).toBe(false)
  })

  it('coalesces rapid start/stop/start without hiding mid-flight', () => {
    const ui = useUiStore()
    ui.startRouteLoading()
    vi.advanceTimersByTime(100)
    expect(ui.routeLoading).toBe(true)

    ui.stopRouteLoading()
    ui.startRouteLoading()
    // Pending again during min-visible wait → stays visible.
    vi.advanceTimersByTime(250)
    expect(ui.routeLoading).toBe(true)

    ui.stopRouteLoading()
    vi.advanceTimersByTime(200)
    expect(ui.routeLoading).toBe(false)
  })
})
