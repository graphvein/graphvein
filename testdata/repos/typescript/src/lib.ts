export function greet(name: string) { return name; }
export const double = (n: number) => n * 2;
export function convert(n: number): number;
export function convert(n: string): string;
export function convert(n: unknown) { return n; }
export default function main() { return greet('world'); }
