import { invoke } from '@tauri-apps/api/core'

export function shadowed() {
  return invoke('greet')
}
