// ADR 0074 P1: JSX attribute names that are reserved words parse (they were
// TS-F2 before), first after the element name and after other attributes.
export function Label(p) {
  return <label class="l" for="x" id="y" default>{p.text}</label>;
}
export function Blend(p) {
  return <feBlend in="SourceGraphic" mode="multiply" new={() => p.go()} />;
}
