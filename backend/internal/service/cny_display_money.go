package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// maxDisplayCNYPerUSD is the website's sanity limit for the display rate.
const maxDisplayCNYPerUSD = 20.0

// cnyDisplayMoney shows ledger amounts, which stay in USD, as the same yuan
// figures the website shows. Its value is CNY per USD. Without a usable rate it
// shows "-" like the website, never a dollar amount or a guessed price.
type cnyDisplayMoney float64

func (m cnyDisplayMoney) available() bool { return m > 0 }

func (m cnyDisplayMoney) format(usd float64) string {
	if !m.available() {
		return "-"
	}
	return fmt.Sprintf("¥%.2f", usd*float64(m))
}

// loadCNYDisplayMoney reads the weekly CNY rate that the currency worker saves
// next to the snapshot the website converts with, so messages and the site show
// one figure.
func loadCNYDisplayMoney(ctx context.Context, settingRepo SettingRepository) cnyDisplayMoney {
	if settingRepo == nil {
		return 0
	}
	raw, err := settingRepo.GetValue(ctx, SettingSubscriptionUSDToCNYRate)
	if err != nil {
		slog.Warn("load CNY display rate failed", "error", err)
		return 0
	}
	rate, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err == nil && rate > 0 && rate < maxDisplayCNYPerUSD {
		return cnyDisplayMoney(rate)
	}
	slog.Warn("stored CNY display rate is not usable", "value", raw)
	return 0
}
