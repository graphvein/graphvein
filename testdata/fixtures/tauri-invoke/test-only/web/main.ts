import { invoke } from '@tauri-apps/api/core'

export async function production() {
  await invoke('ping')
  await invoke('pong')
  await invoke('nope')
}
