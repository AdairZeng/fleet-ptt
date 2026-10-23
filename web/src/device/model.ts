import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'

// Shared coordinates for the bundled GLB, fallback model and DOM controls.
export const screenSize = { width: 1.25, height: 1.67, y: 0.65, z: 0.425 }

export function createDeviceModel(): THREE.Group {
  const model = new THREE.Group()
  model.name = 'xiaolu'
  function part(name: string, geometry: THREE.BufferGeometry, color: number, x: number, y: number, z: number) {
    const mesh = new THREE.Mesh(geometry, new THREE.MeshStandardMaterial({ color, roughness: 0.7 }))
    mesh.name = name
    mesh.position.set(x, y, z)
    model.add(mesh)
    return mesh
  }
  part('body', new RoundedBoxGeometry(1.85, 3.1, 0.72, 5, 0.18), 0x29313b, 0, 0.25, 0)
  part('screen', new THREE.BoxGeometry(screenSize.width + 0.1, screenSize.height + 0.1, 0.06), 0x102a43, 0, screenSize.y, 0.39)
  part('ptt', new THREE.BoxGeometry(0.2, 1.15, 0.3), 0xd97706, -1.02, 0.35, 0)
  part('command', new THREE.BoxGeometry(0.2, 0.52, 0.26), 0x0f766e, 1.02, -0.2, 0)
  part('power', new THREE.CylinderGeometry(0.14, 0.14, 0.24, 24), 0x111827, 0.55, 1.9, 0.05)
  part('indicator', new THREE.SphereGeometry(0.055, 16, 8), 0x38bdf8, 0.55, 1.62, 0.39)
  part('antenna', new THREE.CylinderGeometry(0.07, 0.11, 1.05, 16), 0x111827, -0.55, 2.2, 0)
  part('clip', new THREE.BoxGeometry(0.5, 1.5, 0.13), 0x111827, 0, 0.2, -0.43)
  part('microphone', new THREE.SphereGeometry(0.045, 12, 8), 0x080c12, -0.5, -0.95, 0.36)
  for (let i = 0; i < 4; i++) {
    part(`speaker-${i}`, new THREE.BoxGeometry(0.85, 0.045, 0.035), 0x080c12, 0, -0.52 - i * 0.11, 0.37)
  }
  return model
}
