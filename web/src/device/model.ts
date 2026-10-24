import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'

// The GLB, procedural model and interactive screen share this physical aperture.
export const screenSize = { width: 1.68, height: 2.16, y: 0.31, z: 0.205 }
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
  // The front radius is independent of depth, unlike a rounded box's fillet.
  function enclosure(w: number, h: number, d: number, radius: number) {
    const x = -w / 2, y = -h / 2
    const shape = new THREE.Shape()
    shape.moveTo(x + radius, y)
    shape.lineTo(x + w - radius, y)
    shape.quadraticCurveTo(x + w, y, x + w, y + radius)
    shape.lineTo(x + w, y + h - radius)
    shape.quadraticCurveTo(x + w, y + h, x + w - radius, y + h)
    shape.lineTo(x + radius, y + h)
    shape.quadraticCurveTo(x, y + h, x, y + h - radius)
    shape.lineTo(x, y + radius)
    shape.quadraticCurveTo(x, y, x + radius, y)
    const geometry = new THREE.ExtrudeGeometry(shape, { depth: d - 0.024, bevelEnabled: true, bevelThickness: 0.012, bevelSize: 0.012, bevelSegments: 3, steps: 1, curveSegments: 16 })
    geometry.translate(0, 0, -d / 2 + 0.012)
    geometry.computeVertexNormals()
    return geometry
  }
  const titanium = new THREE.MeshStandardMaterial({ color: 0xc5c2ba, metalness: 0.78, roughness: 0.36 })
  const chamfer = new THREE.MeshStandardMaterial({ color: 0xe8e4dc, metalness: 0.9, roughness: 0.24 })
  const rear = new THREE.MeshStandardMaterial({ color: 0x999a96, metalness: 0.7, roughness: 0.42 })
  const glass = new THREE.MeshPhysicalMaterial({ color: 0x06080b, metalness: 0.05, roughness: 0.23, clearcoat: 1, clearcoatRoughness: 0.12 })
  const recess = new THREE.MeshStandardMaterial({ color: 0x03070c, roughness: 0.8 })

  part('body', enclosure(2.1, 3.38, 0.34, 0.4), titanium, 0, 0, 0)
  part('rear', enclosure(2.035, 3.315, 0.03, 0.37), rear, 0, 0, -0.165)
  part('edge', enclosure(2.065, 3.345, 0.035, 0.385), chamfer, 0, 0, 0.169)
  part('face', enclosure(2.015, 2.55, 0.028, 0.35), glass, 0, 0.37, 0.185)
  part('screen', new THREE.PlaneGeometry(screenSize.width, screenSize.height), glass, 0, screenSize.y, 0.201)

  // A single metal control deck keeps three tactile zones beneath the glass.
  part('command', enclosure(0.57, 0.27, 0.028, 0.13), titanium, 0, -1.19, 0.183)
  const indicator = new THREE.MeshStandardMaterial({ color: 0xb7dde4, emissive: 0xb7dde4, emissiveIntensity: 0.4, roughness: 0.45 })
  part('indicator', box(0.28, 0.012, 0.012, 0.005), indicator, 0, -0.98, 0.188)
  part('ptt', enclosure(0.35, 0.27, 0.028, 0.13), titanium, -0.64, -1.19, 0.183)
  part('power', enclosure(0.35, 0.27, 0.028, 0.13), titanium, 0.64, -1.19, 0.183)
  for (const x of [-0.37, 0.37]) {
    part(`control-seam-${x}`, box(0.008, 0.1, 0.008, 0.002), rear, x, -1.19, 0.174)
  }
  part('speaker', box(0.28, 0.012, 0.01, 0.004), recess, 0, 1.53, 0.203)
  for (const x of [-0.035, 0.035]) {
    part(`microphone-${x}`, new THREE.SphereGeometry(0.008, 12, 6), recess, x, -1.52, 0.177)
  }
  for (const x of [-1.05, 1.05]) {
    part(`antenna-seam-${x}`, new THREE.BoxGeometry(0.008, 0.025, 0.23), rear, x, 0.95, 0)
  }
  part('charging-port', box(0.24, 0.018, 0.07, 0.008), recess, 0, -1.694, 0)
  return model
}
