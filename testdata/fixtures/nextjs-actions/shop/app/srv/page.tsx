import { barrelRemove, starRemove } from '@/lib/barrel';

// A server component that reaches lib/barrel-target.ts and
// lib/star-target.ts only through the barrel (T024).
export default function ServerPage() {
  return <span>{String(barrelRemove)}{String(starRemove)}</span>;
}
