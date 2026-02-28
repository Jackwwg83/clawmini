const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB']

export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '0 B'
  }

  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < BYTE_UNITS.length - 1) {
    size /= 1024
    unitIndex += 1
  }

  const fixed = size >= 10 ? 1 : 2
  return `${size.toFixed(fixed)} ${BYTE_UNITS[unitIndex]}`
}

export function formatPercent(value?: number): string {
  if (value === undefined || !Number.isFinite(value)) {
    return '--'
  }
  return `${value.toFixed(1)}%`
}

export function toProgress(value: number, total: number): number {
  if (!total || total <= 0) {
    return 0
  }
  return Math.max(0, Math.min(100, (value / total) * 100))
}

export function formatDateTime(unixSeconds?: number): string {
  if (!unixSeconds) {
    return '--'
  }
  return new Date(unixSeconds * 1000).toLocaleString('zh-CN', {
    hour12: false,
  })
}

export function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) {
    return '--'
  }
  const day = 24 * 3600
  const hour = 3600
  const minute = 60

  if (seconds >= day) {
    return `${Math.floor(seconds / day)} 天`
  }
  if (seconds >= hour) {
    return `${Math.floor(seconds / hour)} 小时`
  }
  if (seconds >= minute) {
    return `${Math.floor(seconds / minute)} 分钟`
  }
  return `${seconds} 秒`
}

export function formatLastSeen(unixSeconds?: number): string {
  if (!unixSeconds) {
    return '--'
  }

  const diff = Math.max(0, Math.floor(Date.now() / 1000 - unixSeconds))
  if (diff < 60) {
    return `${diff} 秒前`
  }
  if (diff < 3600) {
    return `${Math.floor(diff / 60)} 分钟前`
  }
  if (diff < 86400) {
    return `${Math.floor(diff / 3600)} 小时前`
  }
  return `${Math.floor(diff / 86400)} 天前`
}
