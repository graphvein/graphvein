import { invoke } from '@tauri-apps/api/core'

export function fromTest() {
  return invoke('ping')
}
