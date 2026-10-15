// Home fork: translations for the fork's own phrases. They live in
// home/locales/<lang>.json beside upstream's catalogues (not inside them), so
// upstream releases merge without conflicts in the big locale files; the two
// are joined when the language loads. tools/i18n-check.mjs reads both.
import type { Lang } from '../i18n'

export async function homeCatalogue(code: Lang): Promise<Record<string, string>> {
  try {
    const mod =
      code === 'es'
        ? await import('./locales/es.json')
        : code === 'de'
          ? await import('./locales/de.json')
          : code === 'pt'
            ? await import('./locales/pt.json')
            : code === 'pt-PT'
              ? await import('./locales/pt-PT.json')
              : code === 'fr'
                ? await import('./locales/fr.json')
                : { default: {} }
    return mod.default as Record<string, string>
  } catch {
    return {}
  }
}
