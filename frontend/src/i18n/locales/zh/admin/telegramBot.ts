export default {
  telegramBot: {
    title: 'Telegram 机器人',
    description: '让用户在 Telegram 中查询余额和用量，并发送告警、每日报告和简短通知。',
    setup: {
      title: '机器人',
      description: '用 BotFather 创建机器人并粘贴其 Token。请使用一个没有被其他程序（例如 OpenClaw）读取的机器人：Telegram 只会把消息交给一个程序。',
      enabled: '启用机器人',
      token: '机器人 Token',
      tokenConfigured: '已保存，留空则保持不变。',
      tokenPlaceholder: '123456789:AA…',
      proxy: '代理（可选）',
      proxyHint: '支持 http://、https:// 或 socks5://。仅当服务器无法访问 api.telegram.org 时需要。',
      newsChannel: '官方通知频道链接（可选）',
      newsChannelHint: '用户绑定成功后会看到此链接。',
      group: '讨论组链接（可选）',
      groupHint: '用户发送 /getchatlink 获取此链接。请把机器人设为可添加成员的群管理员，并为该链接开启「需要管理员批准」。机器人会自动通过已绑定用户的申请并拒绝其他人，对它担任管理员的所有群组都生效。',
      bot: '机器人',
      status: '状态',
      receiving: '正在接收消息',
      lastPoll: '最近检查 {time}',
      waiting: '等待首次检查',
      off: '未启用',
      error: '异常（见下方）',
      linkedUsers: '已绑定用户',
      linkedAdmins: '已绑定管理员',
      adminHint: '管理员与普通用户一样在「个人资料 → Telegram」中绑定。告警和报告会发给已绑定的管理员账号。'
    },
    notifications: {
      title: '通知',
      balanceLow: '向用户发送余额不足提醒',
      balanceLowHint: '沿用系统设置中的余额不足阈值，且仅在该提醒开启时发送。',
      adminAlerts: '向管理员发送账号额度和运维告警',
      adminAlertsHint: '与现有告警邮件同时发送。',
      dailyReport: '向管理员发送每日报告',
      reportTime: '发送时间',
      reportTimeHint: '服务器时区：{timezone}'
    },
    save: '保存',
    saved: 'Telegram 设置已保存',
    test: '发送测试消息',
    testSent: '已向 {count} 位管理员发送测试消息',
    testNoAdmins: '还没有已绑定的管理员。请先在个人资料页绑定你自己的账号。',
    notice: {
      title: '简短通知',
      description: '向所有已绑定用户发送一条简短消息，例如倍率分组调整或活动通知。',
      placeholder: '例如：11 月 1 日起 Claude 分组倍率由 0.8x 调整为 1.0x。',
      send: '发送通知',
      confirmTitle: '确认发送此通知？',
      confirmMessage: '将立即发送给 {count} 位已绑定用户。',
      queued: '正在发送给 {count} 位用户',
      needsBot: '请先启用并保存机器人设置。'
    }
  }
}
