// Options for the WeChat login QR code. Shared by the connections dialog and the e2e spec so the
// spec can recompute the exact data URL the component renders.
export const WECHAT_QR_OPTIONS = {
  width: 200,
  margin: 2,
  errorCorrectionLevel: 'M',
  color: { dark: '#000000', light: '#ffffff' },
} as const
