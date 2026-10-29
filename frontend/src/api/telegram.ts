/**
 * Telegram bot account linking (current user).
 */

import { apiClient } from './client'

export interface TelegramLinkStatus {
  available: boolean
  bot_username?: string
  linked: boolean
  telegram_username?: string
  linked_at?: string
  /** Permanent code for /bind; only present while not linked. */
  bind_code?: string
}

export async function getStatus(): Promise<TelegramLinkStatus> {
  const { data } = await apiClient.get<TelegramLinkStatus>('/user/telegram')
  return data
}

export async function resetBindCode(): Promise<TelegramLinkStatus> {
  const { data } = await apiClient.post<TelegramLinkStatus>('/user/telegram/bind-code/reset')
  return data
}

export async function unlink(): Promise<void> {
  await apiClient.delete('/user/telegram')
}

export const telegramAPI = { getStatus, resetBindCode, unlink }

export default telegramAPI
