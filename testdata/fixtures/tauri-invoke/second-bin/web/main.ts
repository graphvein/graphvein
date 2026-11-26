import { invoke } from '@tauri-apps/api/core'
export const a = () => invoke('greet')
