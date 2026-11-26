import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn joins class names and lets later Tailwind classes override earlier
 * conflicting ones, so callers can adjust a shadcn/ui component's look by
 * passing className. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
