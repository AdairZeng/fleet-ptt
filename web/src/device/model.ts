import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'

// The GLB, procedural model and interactive screen share this physical aperture.
export const screenSize = { width: 1.54, height: 1.98, y: 0.42, z: 0.277 }
export const screenPixels = { width: 280, height: 360 }

export function createDeviceModel(): THREE.Group {
  const model = new THREE.Group()
  model.name = 'xiaolu'
  const box = (w: number, h: number, d: number, radius: number) => new RoundedBoxGeometry(w, h, d, 3, radius)
  function part(name: string, geometry: THREE.BufferGeometry, material: THREE.Material, x: number, y: number, z: number) {
    const mesh = new THREE.Mesh(geometry, material)
    mesh.name = name
    mesh.position.set(x, y, z)
    model.add(mesh)
    return mesh
  }
  const titanium = new THREE.MeshStandardMaterial({ color: 0xb9c2cc, metalness: 0.82, roughness: 0.3 })
  const chamfer = new THREE.MeshStandardMaterial({ color: 0xe2e8ee, metalness: 0.9, roughness: 0.2 })
  const rear = new THREE.MeshStandardMaterial({ color: 0x788593, metalness: 0.65, roughness: 0.4 })
  const glass = new THREE.MeshPhysicalMaterial({ color: 0x080d16, metalness: 0.12, roughness: 0.19, clearcoat: 1, clearcoatRoughness: 0.12 })
  const dark = new THREE.MeshStandardMaterial({ color: 0x18202c, metalness: 0.45, roughness: 0.4 })
  const recess = new THREE.MeshStandardMaterial({ color: 0x03070c, roughness: 0.8 })

  part('body', box(2.02, 3.54, 0.48, 0.23), titanium, 0, 0, 0)
  part('rear', box(1.91, 3.42, 0.06, 0.18), rear, 0, 0, -0.232)
  part('edge', box(1.97, 3.49, 0.07, 0.19), chamfer, 0, 0, 0.213)
  part('face', box(1.91, 3.43, 0.055, 0.18), glass, 0, 0, 0.241)
  part('screen', new THREE.PlaneGeometry(screenSize.width, screenSize.height), glass, 0, screenSize.y, 0.272)

  // Flush front controls, each retaining its own named interaction node.
  const command = part('command', new THREE.CylinderGeometry(0.24, 0.24, 0.018, 64), dark, 0, -0.94, 0.281)
  command.rotation.x = Math.PI / 2
  const indicator = new THREE.MeshStandardMaterial({ color: 0x86ddf5, emissive: 0x86ddf5, emissiveIntensity: 0.65, metalness: 0.3, roughness: 0.3 })
  part('indicator', new THREE.TorusGeometry(0.275, 0.013, 8, 80), indicator, 0, -0.94, 0.282)
  part('ptt', box(0.34, 0.13, 0.025, 0.025), titanium, -0.58, -0.94, 0.28)
  part('power', box(0.27, 0.13, 0.025, 0.025), titanium, 0.58, -0.94, 0.28)

  // A subtle earpiece seam and microphone pair replace exposed grille hardware.
  part('speaker', box(0.44, 0.018, 0.012, 0.006), recess, 0, 1.55, 0.274)
  for (const x of [-0.045, 0.045]) {
    part(`microphone-${x}`, new THREE.SphereGeometry(0.011, 12, 6), recess, x, -1.49, 0.27)
  }
  for (const x of [-0.995, 0.995]) {
    part(`antenna-seam-${x}`, new THREE.BoxGeometry(0.012, 0.038, 0.32), rear, x, 1.15, 0)
  }
  part('charging-port', box(0.24, 0.018, 0.07, 0.008), recess, 0, -1.768, 0)
  return model
}
