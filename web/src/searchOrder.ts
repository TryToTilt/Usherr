// The titles you have come first, as the search box draws them, keeping the
// order the server gave inside each group. The arrow keys step through the
// same list, so the highlight moves down the screen one row at a time.
export function libraryFirst<T extends { inLibrary?: boolean }>(list: T[]): T[] {
  return [...list.filter((r) => r.inLibrary), ...list.filter((r) => !r.inLibrary)]
}
