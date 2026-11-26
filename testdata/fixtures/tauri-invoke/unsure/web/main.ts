// Probes q1 and q2 (T015c2 Revision 3): each callable writes
// its parameter where the wrapper lexer would misread the source, except
// u2 (an apostrophe inside a template, no write). Only c2 may link.
import { invoke } from '@tauri-apps/api/core'
declare const ok: boolean
declare const s: string
export function v1(cmd: string) { if (ok) /'/.test(s); [cmd] = ['other']; return invoke(cmd) }
export function v2(cmd: string) { if (ok) /"/.test(s); ({ cmd } = { cmd: 'other' }); return invoke(cmd) }
export function v3(cmd: string) { [cmd] = ['other']; return invoke(cmd) }
export function v4(cmd: string) { let n = 1; n++ / 2, [cmd] = ['other'], n / 1; return invoke(cmd) }
export function v5(cmd: string) { while (ok) /`/.test(s), [cmd] = ['other']; return invoke(cmd) }
export function v6(cmd: string) { c\u{6D}d++; return invoke(cmd) }
export const b1 = () => v1('greet')
export const b2 = () => v2('greet')
export const b3 = () => v3('greet')
export const b4 = () => v4('greet')
export const b5 = () => v5('greet')
export const b6 = () => v6('greet')

export function u1(cmd: string) { if (ok) {} /'/.test(s) && ([cmd] = ['other']); return invoke(cmd) }
export function u2(cmd: string) { const o = { k: `'` }; return invoke(cmd) }
export function u3(cmd: string) { if (ok) /'/.test(s)
  [cmd] = ['other']; return invoke(cmd) }
export const c1 = () => u1('greet')
export const c2 = () => u2('greet')
export const c3 = () => u3('greet')
