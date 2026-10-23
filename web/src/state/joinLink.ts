// 入群二维码的内容是当前页面源站上的 /join/{口令}。
// 系统相机扫开链接，或页面相机读到载荷后，都再用现有的口令加入接口。

const joinCodeAlphabet = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ'

export const invalidJoinQRMessage = '无效的入群二维码'

export const joinQROptions = {
  margin: 2,
  width: 280,
  errorCorrectionLevel: 'M' as const,
}

export type JoinLink = { ok: true; code: string } | { ok: false; message: string }

export function normalizeJoinCode(raw: string): string {
  let out = ''
  for (const ch of raw) {
    if (/\s/u.test(ch)) {
      continue
    }
    out += ch.toUpperCase()
  }
  return out
}

export function isJoinCode(code: string): boolean {
  if (code.length !== 6) {
    return false
  }
  for (const ch of code) {
    if (!joinCodeAlphabet.includes(ch)) {
      return false
    }
  }
  return true
}

export function joinPath(code: string): string {
  return `/join/${encodeURIComponent(code)}`
}

export function joinURL(origin: string, code: string): string {
  return new URL(joinPath(code), origin).href
}

// 从地址栏读取入群链接。普通首页返回 null；路径或 ?join= 不像口令时返回无效提示。
export function readJoinLocation(loc: { pathname: string; search: string }): JoinLink | null {
  const parts = loc.pathname.split('/').filter((part) => part.length > 0)
  if (parts.length > 0 && parts[0].toLowerCase() === 'join') {
    if (parts.length !== 2) {
      return { ok: false, message: invalidJoinQRMessage }
    }
    let segment = parts[1]
    try {
      segment = decodeURIComponent(segment)
    } catch {
      return { ok: false, message: invalidJoinQRMessage }
    }
    return codeLink(segment)
  }
  const query = loc.search.startsWith('?') ? loc.search.slice(1) : loc.search
  const params = new URLSearchParams(query)
  if (!params.has('join')) {
    return null
  }
  return codeLink(params.get('join') ?? '')
}

// 从二维码原文读取口令。空白帧返回 null。可以是 6 位口令，或带 /join/{口令}、?join= 的链接。
export function readJoinPayload(raw: string): JoinLink | null {
  const text = raw.trim()
  if (!text) {
    return null
  }
  if (!text.includes('/') && !text.includes('?') && !text.includes('://') && !text.includes('=')) {
    return codeLink(text)
  }
  try {
    // 扫到的是链接，但不是入群链接：地址栏上的普通首页才用 null，这里要明确提示。
    const link = readJoinLocation(new URL(text, 'https://fleet-ptt.local'))
    return link ?? { ok: false, message: invalidJoinQRMessage }
  } catch {
    return { ok: false, message: invalidJoinQRMessage }
  }
}

export function clearJoinLocation(): void {
  if (!readJoinLocation(window.location)) {
    return
  }
  window.history.replaceState(null, '', '/')
}

function codeLink(raw: string): JoinLink {
  const code = normalizeJoinCode(raw)
  if (!isJoinCode(code)) {
    return { ok: false, message: invalidJoinQRMessage }
  }
  return { ok: true, code }
}
