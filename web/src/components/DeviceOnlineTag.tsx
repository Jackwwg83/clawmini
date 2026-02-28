import { Tag } from 'antd'

export function DeviceOnlineTag({ online }: { online: boolean }) {
  if (online) {
    return <Tag color="success">在线</Tag>
  }
  return <Tag color="default">离线</Tag>
}
