import { dup, onlyValue } from "./star-hub";
import type { Shape } from "./star-hub";
export function useHub(s: Shape) {
  dup();
  return onlyValue() + s.side;
}
