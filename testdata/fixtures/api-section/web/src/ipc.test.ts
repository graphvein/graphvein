import { invoke } from '@tauri-apps/api/core'

export function fromTest() {
  return invoke('t016d_greet')
}
