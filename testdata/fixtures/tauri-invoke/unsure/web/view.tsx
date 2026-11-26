// Probe q1 V7: an apostrophe in JSX text (not a wrapper).
import { invoke } from '@tauri-apps/api/core'
export function V7(cmd: string) { const el = <p>Don't {([cmd] = ['other'])}</p>; return invoke(cmd) }
export const b7 = () => V7('greet')
