// A title as it is compared when someone types in a search box: lower case,
// accents dropped, and "&" read as "and" (so "tom and jerry" finds
// "Tom & Jerry" and the other way round). The same reading the server uses
// when it matches names.
export function looseText(s: string): string {
  return s
    .toLowerCase()
    .replace(/&amp;/g, '&')
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/\s*&\s*/g, ' and ')
    .replace(/\s+/g, ' ')
    .trim()
}

// Whether a typed search matches a name, by looseText.
export function looseIncludes(name: string, needle: string): boolean {
  const n = looseText(needle)
  return n === '' || looseText(name).includes(n)
}
