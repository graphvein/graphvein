// ADR 0074 P2: a bare `&` in JSX text and in a JSX attribute string parses
// (TS-F2 before); entities stay character references.
export function Links(p: { tab: string }) {
  return <a href="/d?tab=s&subtab=n" title="x & y &amp; z">Logs & Metrics {p.tab}</a>;
}
export function AfterAmp() {}
