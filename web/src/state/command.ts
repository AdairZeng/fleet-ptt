// 打开指令前先停上行。再点一次只结束指令，不会重新开麦。
export function commandToggle(commanding: boolean): { commanding: boolean; stopUplink: boolean } {
  if (commanding) {
    return { commanding: false, stopUplink: false }
  }
  return { commanding: true, stopUplink: true }
}
