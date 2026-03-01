export interface CommandPreset {
  key: string
  label: string
  args: string[]
  timeout: number
}

export const COMMAND_PRESETS: CommandPreset[] = [
  {
    key: 'status-json',
    label: '状态检查（status --json）',
    args: ['status', '--json'],
    timeout: 30,
  },
  {
    key: 'doctor-json',
    label: '诊断（doctor --json）',
    args: ['doctor', '--json'],
    timeout: 45,
  },
  {
    key: 'gateway-status',
    label: '网关状态（gateway status）',
    args: ['gateway', 'status'],
    timeout: 20,
  },
  {
    key: 'gateway-restart',
    label: '重启网关（gateway restart）',
    args: ['gateway', 'restart'],
    timeout: 120,
  },
  {
    key: 'plugins-list',
    label: '插件列表（plugins list）',
    args: ['plugins', 'list'],
    timeout: 30,
  },
  {
    key: 'channels-status',
    label: '通道状态（channels status）',
    args: ['channels', 'status'],
    timeout: 30,
  },
  {
    key: 'models-list',
    label: '模型列表（models list）',
    args: ['models', 'list'],
    timeout: 30,
  },
  {
    key: 'health',
    label: '健康检查（health）',
    args: ['health'],
    timeout: 20,
  },
]
