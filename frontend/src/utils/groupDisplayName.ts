/** Platform is displayed separately; retain the server name for identity and filtering. */
export function groupDisplayName(name: string, platform: string): string {
  const prefix = `${platform}-`
  return name.startsWith(prefix) ? name.slice(prefix.length) : name
}
