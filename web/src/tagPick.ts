// Whether a change in the tag box means the person picked a suggestion from
// the list, as opposed to typing letters that happen to spell an existing tag
// ("Kids" on the way to "Kids movies"). Browsers report a list pick as a
// replacement; typing, pasting and deleting have their own input types.
export function pickedSuggestion(inputType: string | undefined, value: string, suggestions: string[]): boolean {
  return inputType === 'insertReplacementText' && suggestions.includes(value)
}
