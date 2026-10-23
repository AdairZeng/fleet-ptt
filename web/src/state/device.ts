// device_id 绑定策略（可复现）：
// 1. 浏览器首次打开时用 crypto.randomUUID() 生成 UUID v4。
// 2. 写入 localStorage 键 fleet-ptt.device_id。同一浏览器配置文件一直复用，不随昵称变化。
// 3. 进入时把 nickname 与这个 device_id 交给 POST /api/v1/session。
// 4. 服务端以 device_id 为唯一键 upsert：同一设备只更新昵称并保持 user_id；新设备新建用户。
// 清除站点数据会生成新的 device_id，从而成为另一个用户。昵称相同也不会合并。

const deviceKey = 'fleet-ptt.device_id'

const uuidV4 =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

export function getOrCreateDeviceID(): string {
  const existing = localStorage.getItem(deviceKey)
  if (existing && uuidV4.test(existing)) {
    return existing
  }
  const id = crypto.randomUUID()
  localStorage.setItem(deviceKey, id)
  return id
}
