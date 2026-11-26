import { invoke as tauriInvoke } from '@tauri-apps/api/core'

// An imported wrapper: forwards its own parameter as the command.
export async function call<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  return tauriInvoke<T>(command, args)
}
