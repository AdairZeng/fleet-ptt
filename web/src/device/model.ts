import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'

// The GLB, procedural model and interactive screen share this physical aperture.
export const screenSize = { width: 1.30, height: 1.82, y: 0.48, z: 0.426 }
export const screenPixels = { width: 260, height: 364 }

export function createDeviceModel(): THREE.Group {
  const model = new THREE.Group()
  model.name = 'xiaolu'
  function part(name: string, geometry: THREE.BufferGeometry, color: number, x: number, y: number, z: number, metalness = 0.2, roughness = 0.45) {
    const mesh = new THREE.Mesh(geometry, new THREE.MeshStandardMaterial({ color, metalness, roughness }))
    mesh.name = name
    mesh.position.set(x, y, z)
    model.add(mesh)
    return mesh
  }
  const box = (w: number, h: number, d: number, radius: number) => new RoundedBoxGeometry(w, h, d, 2, radius)
  part('frame', box(1.86, 3.12, 0.69, 0.17), 0x657783, 0, 0.2, 0, 0.8, 0.28)
  part('body', box(1.78, 3.04, 0.72, 0.16), 0x202b36, 0, 0.2, 0, 0.5, 0.38)
  part('face', box(1.65, 2.88, 0.10, 0.13), 0x111c27, 0, 0.2, 0.338, 0.45, 0.32)
  part('screen-bezel', box(1.46, 1.98, 0.065, 0.09), 0x617888, 0, screenSize.y, 0.38, 0.85, 0.24)
  part('screen', box(screenSize.width + 0.025, screenSize.height + 0.025, 0.045, 0.035), 0x060f18, 0, screenSize.y, 0.402, 0.15, 0.26)
  part('ptt', box(0.14, 0.80, 0.34, 0.055), 0x38556a, -0.955, 0.36, 0.01, 0.65)
  part('command', box(0.14, 0.38, 0.32, 0.05), 0x2f7182, 0.955, -0.17, 0.01, 0.65)
  part('power', new THREE.CylinderGeometry(0.12, 0.13, 0.17, 32), 0x496170, 0.52, 1.82, 0, 0.8, 0.3)
  const lamp = part('indicator', box(0.78, 0.025, 0.025, 0.01), 0x65deff, 0, 1.59, 0.4)
  const material = lamp.material as THREE.MeshStandardMaterial
  material.emissive.setHex(0x65deff)
  material.emissiveIntensity = 1.3
  part('antenna-base', new THREE.CylinderGeometry(0.12, 0.13, 0.15, 32), 0x657783, -0.52, 1.82, 0, 0.8)
  part('antenna', new THREE.CylinderGeometry(0.065, 0.09, 0.58, 24), 0x17212b, -0.52, 2.14, 0)
  part('antenna-cap', new THREE.SphereGeometry(0.065, 16, 8), 0x17212b, -0.52, 2.43, 0)
  part('clip', box(0.48, 1.35, 0.14, 0.04), 0x16222d, 0, 0.15, -0.42, 0.65)
  part('speaker-panel', box(1.36, 0.47, 0.035, 0.08), 0x09121c, 0, -0.77, 0.398)
  for (let row = 0; row < 3; row++) {
    for (let col = 0; col < 9; col++) {
      part(`speaker-${row}-${col}`, box(0.065, 0.025, 0.015, 0.01), 0x425566, (col - 4) * 0.125, -0.68 - row * 0.085, 0.42, 0.5)
    }
  }
  for (const x of [-0.69, 0.69]) {
    for (const y of [-1.08, 1.48]) {
      const screw = part(`fastener-${x}-${y}`, new THREE.CylinderGeometry(0.025, 0.025, 0.015, 12), 0x7c8f9c, x, y, 0.4, 0.9)
      screw.rotation.x = Math.PI / 2
    }
    for (let i = 0; i < 5; i++) {
      part(`grip-${x}-${i}`, box(0.05, 0.17, 0.07, 0.015), 0x101923, Math.sign(x) * 0.85, -0.54 - i * 0.12, 0.24)
    }
  }
  part('microphone', new THREE.SphereGeometry(0.025, 12, 8), 0x03080e, -0.38, -1.12, 0.395)
  part('signature', box(0.32, 0.018, 0.018, 0.008), 0x65deff, 0, -1.12, 0.4, 0.5)
  return model
}
