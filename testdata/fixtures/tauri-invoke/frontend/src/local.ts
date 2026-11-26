import { invoke } from '@tauri-apps/api/core'

// A local, unexported wrapper.
const send = (command: string) => invoke(command)

send('greet')
send('greet')
send('greet')

export function shadowedLocal() {
  const send = (name: string) => name
  return send('greet')
}
