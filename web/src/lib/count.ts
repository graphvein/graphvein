// Counts in fixed-width places (the top bar's badge and Re-index slot).

const plain = new Intl.NumberFormat(undefined);
const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumSignificantDigits: 3 });

/** formatCount shows counts up to 9,999 in full and larger ones with three
 * significant digits ("12.3K", "123K", "1.23M"), so a count never needs
 * more room than the widths reserved by COUNT_SIZER_SAMPLES. */
export function formatCount(n: number): string {
  return n < 10_000 ? plain.format(n) : compact.format(n);
}

/** COUNT_SIZER_SAMPLES are the widest forms formatCount produces per
 * magnitude; places that must not change width reserve room for each. */
export const COUNT_SIZER_SAMPLES: readonly string[] = [formatCount(9_999), formatCount(99_900), formatCount(9_990_000)];
