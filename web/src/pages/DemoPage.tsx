import { LeftOutlined, RightOutlined } from '@ant-design/icons'
import { Button, Card, Col, Progress, Row, Space, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'

interface DemoStep {
  key: string
  title: string
  durationSeconds: number
  instruction: string
  focusArea: string
}

const DEMO_STEPS: DemoStep[] = [
  {
    key: 'dashboard',
    title: 'Step 1: 打开 Dashboard',
    durationSeconds: 30,
    instruction: '展示设备总览，说明在线设备数量与实时状态。',
    focusArea: '顶部统计卡 + 设备列表卡片区',
  },
  {
    key: 'device-detail',
    title: 'Step 2: 点击任意设备',
    durationSeconds: 30,
    instruction: '演示设备详情中的网关状态、系统资源与诊断面板。',
    focusArea: '设备详情页左上信息卡 + 中部功能卡',
  },
  {
    key: 'onboarding',
    title: 'Step 3: 接入新设备',
    durationSeconds: 60,
    instruction: '生成 Join Token，复制安装命令，说明设备自动上线流程。',
    focusArea: '接入设备页面中的令牌和命令区域',
  },
  {
    key: 'im-config',
    title: 'Step 4: 配置钉钉',
    durationSeconds: 120,
    instruction: '进入 IM 配置向导，逐步填写凭据并执行自动配置步骤。',
    focusArea: 'IM 向导步骤条 + 执行日志',
  },
  {
    key: 'realtime-message',
    title: 'Step 5: 演示实时回传',
    durationSeconds: 60,
    instruction: '在钉钉发送消息，展示管理台实时状态和日志变化。',
    focusArea: '设备详情页日志查看 + 通道状态',
  },
]

function formatTimer(seconds: number): string {
  const safe = Math.max(0, seconds)
  const minute = Math.floor(safe / 60)
  const second = safe % 60
  return `${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}`
}

export function DemoPage() {
  const [stepIndex, setStepIndex] = useState(0)
  const [remaining, setRemaining] = useState(DEMO_STEPS[0].durationSeconds)

  const currentStep = DEMO_STEPS[stepIndex]
  const totalSeconds = useMemo(() => DEMO_STEPS.reduce((sum, item) => sum + item.durationSeconds, 0), [])
  const elapsedSeconds = useMemo(
    () => DEMO_STEPS.slice(0, stepIndex).reduce((sum, item) => sum + item.durationSeconds, 0) + (currentStep.durationSeconds - remaining),
    [currentStep.durationSeconds, remaining, stepIndex],
  )

  useEffect(() => {
    setRemaining(currentStep.durationSeconds)
  }, [currentStep.durationSeconds, stepIndex])

  useEffect(() => {
    const timer = window.setInterval(() => {
      setRemaining((prev) => {
        if (prev <= 1) {
          return 0
        }
        return prev - 1
      })
    }, 1000)
    return () => window.clearInterval(timer)
  }, [stepIndex])

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card>
        <Space direction="vertical" size={4}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            Demo Script
          </Typography.Title>
          <Typography.Text type="secondary">5 分钟演示脚本，按步骤推进展示核心能力。</Typography.Text>
        </Space>
      </Card>

      <Card>
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Typography.Text>
            总进度：{formatTimer(elapsedSeconds)} / {formatTimer(totalSeconds)}
          </Typography.Text>
          <Progress percent={Math.round((elapsedSeconds / totalSeconds) * 100)} />
        </Space>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={16}>
          <Card>
            <Space direction="vertical" size={14} style={{ width: '100%' }}>
              <Typography.Title level={4} style={{ margin: 0 }}>
                {currentStep.title}
              </Typography.Title>
              <Typography.Paragraph style={{ marginBottom: 0 }}>{currentStep.instruction}</Typography.Paragraph>
              <Typography.Text type="secondary">建议展示区域：{currentStep.focusArea}</Typography.Text>
              <div className="demo-focus-box">
                <Typography.Text>Screenshot / Spotlight 区域</Typography.Text>
              </div>
            </Space>
          </Card>
        </Col>

        <Col xs={24} xl={8}>
          <Card>
            <Space direction="vertical" size={14} style={{ width: '100%' }}>
              <Typography.Text type="secondary">当前步骤倒计时</Typography.Text>
              <Typography.Title level={2} style={{ margin: 0 }}>
                {formatTimer(remaining)}
              </Typography.Title>
              <Space>
                <Button
                  icon={<LeftOutlined />}
                  disabled={stepIndex === 0}
                  onClick={() => setStepIndex((prev) => Math.max(0, prev - 1))}
                >
                  上一步
                </Button>
                <Button
                  type="primary"
                  icon={<RightOutlined />}
                  disabled={stepIndex === DEMO_STEPS.length - 1}
                  onClick={() => setStepIndex((prev) => Math.min(DEMO_STEPS.length - 1, prev + 1))}
                >
                  下一步
                </Button>
              </Space>
            </Space>
          </Card>
        </Col>
      </Row>
    </Space>
  )
}
