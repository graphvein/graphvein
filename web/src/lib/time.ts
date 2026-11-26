// Times from the API are UTC RFC 3339; the UI shows them in local time.

const formatter = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

const shortFormatter = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});

/** formatTime shows an RFC 3339 time in the browser's locale and time zone,
 * with seconds (for "checked at"). An unparsable value is shown as is. */
export function formatTime(value: string | Date): string {
  const date = typeof value === "string" ? new Date(value) : value;
  return Number.isNaN(date.getTime()) ? String(value) : formatter.format(date);
}

/** formatShortTime is formatTime without year and seconds, for badges. */
export function formatShortTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : shortFormatter.format(date);
}
