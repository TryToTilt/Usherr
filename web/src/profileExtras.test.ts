import { describe, expect, it } from 'vitest'
import { advancedCount, extrasDraft, extrasFrom, extrasProblem, simpleQualityOf, sliderGB, sliderValue } from './profileExtras'

const msg = { size: () => 'size', order: () => 'order', whole: () => 'whole' }

describe('profile extras', () => {
  it('round-trips the Advanced options through the form', () => {
    const e = {
      kind: 'movies' as const,
      tierSizes: { 'Remux-2160p': { minGB: 40, preferredGB: 70, maxGB: 120 } },
      preferSmaller: true,
      preferGroups: ['FraMeSToR', 'HONE'],
      neverGroups: ['YIFY'],
      audioLanguage: 'Japanese,English',
      indexers: [3],
      protocol: 'usenet' as const,
      minAgeMinutes: 30,
      upgradeDays: 90,
    }
    const back = extrasFrom(extrasDraft(e))
    expect(back.tierSizes).toEqual(e.tierSizes)
    expect(back.preferGroups).toEqual(['FraMeSToR', 'HONE'])
    expect(back.neverGroups).toEqual(['YIFY'])
    expect(back.minAgeMinutes).toBe(30)
    expect(back.upgradeDays).toBe(90)
    expect(back.protocol).toBe('usenet')
    expect(advancedCount(extrasDraft(e))).toBe(5)
    expect(advancedCount(extrasDraft(undefined))).toBe(0)
  })

  it('drops empty size rows and refuses sizes out of order', () => {
    const d = extrasDraft(undefined)
    d.sizes = { 'Bluray-1080p': { min: '', pref: '', max: '' } }
    expect(extrasFrom(d).tierSizes).toEqual({})
    d.sizes = { 'Bluray-1080p': { min: '20', pref: '', max: '10' } }
    expect(extrasProblem(d, msg)).toBe('order')
    d.sizes = { 'Remux-2160p': { min: '40', pref: '70', max: '120' } }
    expect(extrasProblem(d, msg)).toBeNull()
    d.upgradeDays = '1.5'
    expect(extrasProblem(d, msg)).toBe('whole')
  })

  it('knows the Simple quality choices', () => {
    expect(simpleQualityOf(['Bluray-1080p', 'WEBDL-1080p'], false)).toBe('hd')
    expect(simpleQualityOf(['WEBDL-1080p', 'Bluray-1080p', 'WEBDL-2160p', 'Bluray-2160p', 'Remux-2160p'], false)).toBe('4kfb')
    expect(simpleQualityOf(['Bluray-1080p', 'WEBDL-1080p'], true)).toBeNull()
    expect(simpleQualityOf(['SDTV'], false)).toBeNull()
  })

  it('maps the size slider, with no limit at its end', () => {
    expect(sliderValue(0)).toBe(151)
    expect(sliderGB(151)).toBe(0)
    expect(sliderValue(200)).toBe(150)
    expect(sliderGB(60)).toBe(60)
  })
})
