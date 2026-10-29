package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// Telegram messages are plain text (no parse mode), so nothing needs escaping.

func tgText(lang, en, zh string) string {
	if lang == "zh" {
		return zh
	}
	return en
}

func formatTelegramCount(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			_ = b.WriteByte(',')
		}
		_, _ = b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func formatTelegramTokens(v int64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(v)/1e9)
	case v >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(v)/1e6)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", float64(v)/1e3)
	default:
		return strconv.FormatInt(v, 10)
	}
}

func formatTelegramUSD(v float64) string { return fmt.Sprintf("$%.2f", v) }

// ─── Notifications ───

func telegramBalanceLowText(lang, site string, balance, threshold float64, rechargeURL string) string {
	text := tgText(lang,
		fmt.Sprintf("⚠️ %s: your balance is low\nBalance: %s (alert below %s)", site, formatTelegramUSD(balance), formatTelegramUSD(threshold)),
		fmt.Sprintf("⚠️ %s：余额不足\n当前余额：%s（低于 %s 提醒）", site, formatTelegramUSD(balance), formatTelegramUSD(threshold)),
	)
	if rechargeURL != "" {
		text += tgText(lang, "\nTop up: ", "\n充值：") + rechargeURL
	}
	return text
}

func telegramQuotaDimLabel(lang, dim string) string {
	switch dim {
	case quotaDimDaily:
		return tgText(lang, "Daily quota", "日限额")
	case quotaDimWeekly:
		return tgText(lang, "Weekly quota", "周限额")
	case quotaDimTotal:
		return tgText(lang, "Total quota", "总限额")
	}
	return dim
}

func telegramAccountQuotaText(lang, site, accountName, platform, dim string, used, limit float64) string {
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return tgText(lang,
		fmt.Sprintf("⚠️ %s: account quota alert\nAccount: %s (%s)\n%s: %s used of %s, %s left",
			site, accountName, platform, telegramQuotaDimLabel(lang, dim), formatTelegramUSD(used), formatTelegramUSD(limit), formatTelegramUSD(remaining)),
		fmt.Sprintf("⚠️ %s：账号额度提醒\n账号：%s（%s）\n%s：已用 %s / %s，剩余 %s",
			site, accountName, platform, telegramQuotaDimLabel(lang, dim), formatTelegramUSD(used), formatTelegramUSD(limit), formatTelegramUSD(remaining)),
	)
}

func telegramOpsAlertText(lang, site string, rule *OpsAlertRule, event *OpsAlertEvent) string {
	value := "-"
	if event.MetricValue != nil {
		value = fmt.Sprintf("%.2f", *event.MetricValue)
	}
	threshold := fmt.Sprintf("%.2f", rule.Threshold)
	if event.ThresholdValue != nil {
		threshold = fmt.Sprintf("%.2f", *event.ThresholdValue)
	}
	text := tgText(lang,
		fmt.Sprintf("🚨 %s ops alert [%s]\n%s\n%s: %s (threshold %s %s)", site, rule.Severity, rule.Name, rule.MetricType, value, rule.Operator, threshold),
		fmt.Sprintf("🚨 %s 运维告警 [%s]\n%s\n%s：%s（阈值 %s %s）", site, rule.Severity, rule.Name, rule.MetricType, value, rule.Operator, threshold),
	)
	if d := strings.TrimSpace(event.Description); d != "" {
		text += "\n" + d
	}
	return text
}

func telegramTestText(lang, site string) string {
	return tgText(lang,
		fmt.Sprintf("✅ %s: Telegram bot is working. Admin alerts will arrive here.", site),
		fmt.Sprintf("✅ %s：Telegram 机器人工作正常，管理员通知会发到这里。", site),
	)
}

func telegramNoticeText(site, text string) string {
	return fmt.Sprintf("📢 %s\n\n%s", site, text)
}

// ─── Command replies ───

func telegramHelpText(lang string, isAdmin, hasGroup bool) string {
	text := tgText(lang,
		"Commands:\n/balance - balance and subscriptions\n/usage - today and total usage\n/keys - API keys",
		"可用命令：\n/balance - 余额与订阅\n/usage - 今日与累计用量\n/keys - API 密钥",
	)
	if hasGroup {
		text += tgText(lang, "\n/getchatlink - discussion group link", "\n/getchatlink - 讨论组链接")
	}
	text += tgText(lang, "\n/unbind - unlink this chat", "\n/unbind - 解除绑定")
	if isAdmin {
		text += tgText(lang, "\n\nAdmin:\n/report - today's site report", "\n\n管理员：\n/report - 今日站点报告")
	}
	return text
}

func telegramNotLinkedText(lang, site string) string {
	return tgText(lang,
		fmt.Sprintf("This chat is not linked to a %s account yet.\nSign in to %s, copy your bind code from Profile → Telegram, then send:\n/bind YOUR_CODE", site, site),
		fmt.Sprintf("此聊天尚未绑定 %s 账号。\n请登录 %s，在「个人资料 → Telegram」复制你的绑定码，然后发送：\n/bind 你的绑定码", site, site),
	)
}

func telegramLinkedText(lang, maskedEmail, newsURL string) string {
	text := tgText(lang,
		fmt.Sprintf("✅ Linked to %s.\nSend /balance to check your balance, or /help for all commands.", maskedEmail),
		fmt.Sprintf("✅ 你已绑定成功：%s。\n发送 /balance 查询余额，发送 /help 查看全部命令。", maskedEmail),
	)
	if newsURL != "" {
		text += tgText(lang, "\n\n📢 Follow our announcements: ", "\n\n📢 请关注官方通知频道：") + newsURL
	}
	return text
}

func telegramLinkInvalidText(lang, site string) string {
	return tgText(lang,
		fmt.Sprintf("❌ This bind code is not valid. Copy it again from Profile → Telegram on %s.", site),
		fmt.Sprintf("❌ 绑定码无效，请在 %s 的「个人资料 → Telegram」重新复制。", site),
	)
}

func telegramLinkedElsewhereText(lang string) string {
	return tgText(lang,
		"❌ This account is already linked to another Telegram account. Unlink it on the website (Profile → Telegram) or send /unbind from that account first.",
		"❌ 该账号已绑定其他 Telegram 账号。请先在网站「个人资料 → Telegram」解除绑定，或在原 Telegram 账号中发送 /unbind。",
	)
}

func telegramGroupLinkText(lang, site, groupURL string) string {
	if groupURL == "" {
		return tgText(lang, "There is no discussion group yet.", "暂未开放讨论组。")
	}
	return tgText(lang,
		fmt.Sprintf("💬 The %s discussion group: %s\nRequest to join; linked accounts are approved automatically.", site, groupURL),
		fmt.Sprintf("💬 %s 的讨论组链接是：%s\n申请加入后，已绑定的账号会自动通过审核。", site, groupURL),
	)
}

func telegramGroupApprovedText(lang, site string) string {
	return tgText(lang,
		fmt.Sprintf("✅ Your account passed the automatic check and joined the %s discussion group.\nPlease follow the group rules.", site),
		fmt.Sprintf("✅ 你的账号已经通过系统自动审核并成功加入 %s 讨论组。\n请遵守群组规范。", site),
	)
}

func telegramGroupDeclinedText(lang, site string) string {
	return tgText(lang,
		fmt.Sprintf("Only linked %s accounts can join the discussion group. Copy your bind code from Profile → Telegram, send /bind YOUR_CODE to me, then request to join again.", site),
		fmt.Sprintf("仅已绑定 %s 账号的用户可以加入讨论组。请在「个人资料 → Telegram」复制绑定码，向我发送 /bind 你的绑定码，然后重新申请加入。", site),
	)
}

func telegramUnlinkedText(lang string) string {
	return tgText(lang, "This chat has been unlinked.", "已解除绑定。")
}

func telegramAccountUnavailableText(lang string) string {
	return tgText(lang, "This account is disabled or no longer exists.", "该账号已被禁用或不存在。")
}

func telegramTryLaterText(lang string) string {
	return tgText(lang, "Something went wrong. Please try again later.", "出错了，请稍后再试。")
}

func telegramAdminOnlyText(lang string) string {
	return tgText(lang, "This command is for admins only.", "该命令仅限管理员使用。")
}

func telegramBalanceText(lang string, user *User, subs []UserSubscription) string {
	var b strings.Builder
	_, _ = b.WriteString(tgText(lang, "💰 Balance: ", "💰 余额："))
	_, _ = b.WriteString(formatTelegramUSD(user.Balance))
	if len(subs) == 0 {
		return b.String()
	}
	_, _ = b.WriteString(tgText(lang, "\n\nSubscriptions:", "\n\n订阅："))
	for _, sub := range subs {
		name := fmt.Sprintf("#%d", sub.GroupID)
		if sub.Group != nil && sub.Group.Name != "" {
			name = sub.Group.Name
		}
		_, _ = fmt.Fprintf(&b, tgText(lang, "\n• %s, expires %s", "\n• %s，%s 到期"), name, sub.ExpiresAt.Format("2006-01-02"))
	}
	return b.String()
}

func telegramUsageText(lang string, st *usagestats.UserDashboardStats) string {
	return tgText(lang,
		fmt.Sprintf("📈 Usage\nToday: %s requests · %s tokens · %s\nTotal: %s requests · %s tokens · %s",
			formatTelegramCount(st.TodayRequests), formatTelegramTokens(st.TodayTokens), formatTelegramUSD(st.TodayActualCost),
			formatTelegramCount(st.TotalRequests), formatTelegramTokens(st.TotalTokens), formatTelegramUSD(st.TotalActualCost)),
		fmt.Sprintf("📈 用量\n今日：%s 次请求 · %s tokens · %s\n累计：%s 次请求 · %s tokens · %s",
			formatTelegramCount(st.TodayRequests), formatTelegramTokens(st.TodayTokens), formatTelegramUSD(st.TodayActualCost),
			formatTelegramCount(st.TotalRequests), formatTelegramTokens(st.TotalTokens), formatTelegramUSD(st.TotalActualCost)),
	)
}

func telegramKeysText(lang string, keys []APIKey, total int64) string {
	if len(keys) == 0 {
		return tgText(lang, "You have no API keys yet.", "你还没有 API 密钥。")
	}
	var b strings.Builder
	_, _ = b.WriteString(tgText(lang, "🔑 API keys", "🔑 API 密钥"))
	for _, k := range keys {
		status := tgText(lang, "active", "启用")
		if !k.IsActive() {
			status = tgText(lang, "disabled", "停用")
		}
		quota := tgText(lang, "no limit", "不限额")
		if k.Quota > 0 {
			quota = formatTelegramUSD(k.QuotaUsed) + " / " + formatTelegramUSD(k.Quota)
		} else if k.QuotaUsed > 0 {
			quota = tgText(lang, "used ", "已用 ") + formatTelegramUSD(k.QuotaUsed)
		}
		_, _ = fmt.Fprintf(&b, "\n• %s — %s · %s", k.Name, status, quota)
		if k.ExpiresAt != nil {
			_, _ = fmt.Fprintf(&b, tgText(lang, " · expires %s", " · %s 到期"), k.ExpiresAt.Format("2006-01-02"))
		}
	}
	if total > int64(len(keys)) {
		_, _ = fmt.Fprintf(&b, tgText(lang, "\n…and %d more", "\n…另有 %d 个"), total-int64(len(keys)))
	}
	return b.String()
}

func telegramDailyReportText(lang, site, day string, st *usagestats.DashboardStats) string {
	return tgText(lang,
		fmt.Sprintf("📊 %s report · %s\nRequests: %s · Tokens: %s\nCharged: %s · Upstream cost: %s\nUsers: %s total · %s new · %s active\nAccounts: %s ok · %s error · %s rate-limited · %s overloaded",
			site, day,
			formatTelegramCount(st.TodayRequests), formatTelegramTokens(st.TodayTokens),
			formatTelegramUSD(st.TodayActualCost), formatTelegramUSD(st.TodayAccountCost),
			formatTelegramCount(st.TotalUsers), formatTelegramCount(st.TodayNewUsers), formatTelegramCount(st.ActiveUsers),
			formatTelegramCount(st.NormalAccounts), formatTelegramCount(st.ErrorAccounts), formatTelegramCount(st.RateLimitAccounts), formatTelegramCount(st.OverloadAccounts)),
		fmt.Sprintf("📊 %s 日报 · %s\n请求：%s · Tokens：%s\n用户扣费：%s · 上游成本：%s\n用户：共 %s · 新增 %s · 活跃 %s\n账号：正常 %s · 异常 %s · 限流 %s · 过载 %s",
			site, day,
			formatTelegramCount(st.TodayRequests), formatTelegramTokens(st.TodayTokens),
			formatTelegramUSD(st.TodayActualCost), formatTelegramUSD(st.TodayAccountCost),
			formatTelegramCount(st.TotalUsers), formatTelegramCount(st.TodayNewUsers), formatTelegramCount(st.ActiveUsers),
			formatTelegramCount(st.NormalAccounts), formatTelegramCount(st.ErrorAccounts), formatTelegramCount(st.RateLimitAccounts), formatTelegramCount(st.OverloadAccounts)),
	)
}
