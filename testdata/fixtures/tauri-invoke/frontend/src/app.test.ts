import { invoke } from '@tauri-apps/api/core'

export function fromTest() {
  return invoke('greet')
}

// A test-scope call may reach a cfg(test) command.
export function testCommand() {
  return invoke('test_cmd')
}
