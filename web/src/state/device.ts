const credentialKey = 'fleet-ptt.device-credential'

export type DeviceCredential = { device_id: string; mac_id: string; device_credential: string }

export function loadDeviceCredential(): DeviceCredential | null {
  const raw = localStorage.getItem(credentialKey)
  if (!raw) return null
  try {
    const value = JSON.parse(raw) as Partial<DeviceCredential>
    return typeof value.device_id === 'string' && typeof value.mac_id === 'string' && typeof value.device_credential === 'string'
      ? value as DeviceCredential : null
  } catch { return null }
}

export async function registerDevice(sessionToken?: string): Promise<DeviceCredential> {
  const headers = new Headers()
  if (sessionToken) headers.set('Authorization', `Bearer ${sessionToken}`)
  const response = await fetch('/api/v1/device/register', { method: 'POST', headers })
  const body = await response.json()
  if (!response.ok || !body.device_id || !body.mac_id || !body.device_credential) {
    throw new Error(body.error || '设备注册失败')
  }
  const credential = { device_id: body.device_id, mac_id: body.mac_id, device_credential: body.device_credential }
  localStorage.setItem(credentialKey, JSON.stringify(credential))
  return credential
}
