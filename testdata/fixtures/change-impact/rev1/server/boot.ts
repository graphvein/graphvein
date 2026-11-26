import { Formatter } from './items';

Formatter.first(['boot']);

export function legacy(helpers: any) {
  return helpers.formatRow('boot');
}
