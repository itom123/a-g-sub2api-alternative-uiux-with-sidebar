export default {
  telegramBot: {
    title: 'Telegram Bot',
    description: 'Let users check balance and usage in Telegram, and send alerts, daily reports and short notices.',
    setup: {
      title: 'Bot',
      description: 'Create a bot with BotFather and paste its token. Use a bot that no other program reads, such as OpenClaw: Telegram delivers a bot’s messages to only one program.',
      enabled: 'Enable bot',
      token: 'Bot token',
      tokenConfigured: 'Saved. Leave blank to keep it.',
      tokenPlaceholder: '123456789:AA…',
      proxy: 'Proxy (optional)',
      proxyHint: 'http://, https:// or socks5://. Only needed if this server cannot reach api.telegram.org.',
      newsChannel: 'News channel link (optional)',
      newsChannelHint: 'Shown to users right after they link their account.',
      group: 'Discussion group link (optional)',
      groupHint: 'Users get it with /getchatlink. Make the bot a group admin that can add members, and turn on "Request admin approval" for this link. The bot approves linked users and declines everyone else, in every group where it is an admin.',
      bot: 'Bot',
      status: 'Status',
      receiving: 'Receiving messages',
      lastPoll: 'last checked {time}',
      waiting: 'Waiting for the first check',
      off: 'Off',
      error: 'Problem (see below)',
      linkedUsers: 'Linked users',
      linkedAdmins: 'Linked admins',
      adminHint: 'Admins link Telegram the same way users do: Profile → Telegram. Alerts and reports go to linked admin accounts.'
    },
    notifications: {
      title: 'Notifications',
      balanceLow: 'Low-balance alerts to users',
      balanceLowHint: 'Uses the low-balance threshold from System Settings, and only when that alert is on.',
      adminAlerts: 'Account quota and ops alerts to admins',
      adminAlertsHint: 'Sent alongside the existing alert emails.',
      dailyReport: 'Daily report to admins',
      reportTime: 'Send at',
      reportTimeHint: 'Server time zone: {timezone}'
    },
    save: 'Save',
    saved: 'Telegram settings saved',
    test: 'Send test message',
    testSent: 'Test message sent to {count} admin(s)',
    testNoAdmins: 'No linked admins yet. Link your own account on the Profile page first.',
    notice: {
      title: 'Short notice',
      description: 'Send a short message to every linked user, for example a rate group change or an event.',
      placeholder: 'e.g. From Nov 1 the Claude group rate changes from 0.8x to 1.0x.',
      send: 'Send notice',
      confirmTitle: 'Send this notice?',
      confirmMessage: 'It will be sent to {count} linked user(s) right away.',
      queued: 'Sending to {count} user(s)',
      needsBot: 'Enable and save the bot first.'
    }
  }
}
