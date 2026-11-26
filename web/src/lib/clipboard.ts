// Writing to the clipboard where the API may be missing (an insecure
// context or a test DOM): the caller shows whether it worked, and the
// text stays selectable on the page either way.

/** copyText copies text and resolves whether it worked; it never rejects. */
export function copyText(text: string): Promise<boolean> {
  try {
    return navigator.clipboard.writeText(text).then(
      () => true,
      () => false,
    );
  } catch {
    return Promise.resolve(false);
  }
}
