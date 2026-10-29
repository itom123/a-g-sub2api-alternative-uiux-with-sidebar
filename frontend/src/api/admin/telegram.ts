/**
 * Admin Telegram bot settings and short notices.
 */

import { apiClient } from '../client'

export interface TelegramBotSettings {
  enabled: boolean
  token_configured: boolean
  bot_username: string
  proxy_url: string
  notify_balance_low: boolean
  notify_admin_alerts: boolean
  daily_report_enabled: boolean
  daily_report_time: string
  news_channel_url: string
  group_invite_url: string
  timezone: string
  linked_users: number
  linked_admins: number
  last_error?: string
  last_poll_at?: string
  broadcast_max_length: number
}

export interface TelegramBotSettingsInput {
  enabled: boolean
  /** Empty keeps the stored token. */
  bot_token: string
  proxy_url: string
  notify_balance_low: boolean
  notify_admin_alerts: boolean
  daily_report_enabled: boolean
  daily_report_time: string
  news_channel_url: string
  group_invite_url: string
}

export async function getSettings(): Promise<TelegramBotSettings> {
  const { data } = await apiClient.get<TelegramBotSettings>('/admin/telegram/settings')
  return data
}

export async function updateSettings(input: TelegramBotSettingsInput): Promise<TelegramBotSettings> {
  const { data } = await apiClient.put<TelegramBotSettings>('/admin/telegram/settings', input)
  return data
}

export async function sendTest(): Promise<{ sent: number }> {
  const { data } = await apiClient.post<{ sent: number }>('/admin/telegram/test')
  return data
}

export async function broadcast(text: string): Promise<{ recipients: number }> {
  const { data } = await apiClient.post<{ recipients: number }>('/admin/telegram/broadcast', { text })
  return data
}

export const telegramAdminAPI = { getSettings, updateSettings, sendTest, broadcast }

export default telegramAdminAPI
