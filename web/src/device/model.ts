import * as THREE from 'three'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'

// The GLB, procedural model and interactive screen share this physical aperture.
export const screenSize = { width: 1.68, height: 2.16, y: 0.35, z: 0.205 }
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
  const titanium = new THREE.MeshStandardMaterial({ color: 0xabb2b8, metalness: 0.72, roughness: 0.32 })
  const chamfer = new THREE.MeshStandardMaterial({ color: 0xe2e8ee, metalness: 0.9, roughness: 0.2 })
  const rear = new THREE.MeshStandardMaterial({ color: 0x788593, metalness: 0.65, roughness: 0.4 })
  const glass = new THREE.MeshPhysicalMaterial({ color: 0x070a0e, metalness: 0.05, roughness: 0.27, clearcoat: 1, clearcoatRoughness: 0.12 })
  const dark = new THREE.MeshStandardMaterial({ color: 0x18202c, metalness: 0.45, roughness: 0.4 })
  const recess = new THREE.MeshStandardMaterial({ color: 0x03070c, roughness: 0.8 })

  part('body', enclosure(2.04, 3.5, 0.34, 0.35), titanium, 0, 0, 0)
  part('rear', enclosure(1.97, 3.43, 0.03, 0.33), rear, 0, 0, -0.165)
  part('edge', enclosure(2.01, 3.47, 0.035, 0.34), chamfer, 0, 0, 0.169)
  part('face', enclosure(1.965, 3.425, 0.028, 0.32), glass, 0, 0, 0.185)
  part('screen', new THREE.PlaneGeometry(screenSize.width, screenSize.height), glass, 0, screenSize.y, 0.201)

  // Flush front controls, each retaining its own named interaction node.
  const command = part('command', new THREE.CylinderGeometry(0.215, 0.215, 0.014, 64), dark, 0, -1.12, 0.211)
  command.rotation.x = Math.PI / 2
  const indicator = new THREE.MeshStandardMaterial({ color: 0x86ddf5, emissive: 0x86ddf5, emissiveIntensity: 0.65, metalness: 0.3, roughness: 0.3 })
  part('indicator', new THREE.TorusGeometry(0.243, 0.007, 8, 80), indicator, 0, -1.12, 0.214)
  part('ptt', enclosure(0.34, 0.21, 0.026, 0.1), dark, -0.59, -1.12, 0.207)
  part('power', enclosure(0.3, 0.21, 0.026, 0.1), dark, 0.59, -1.12, 0.207)

  part('speaker', box(0.3, 0.012, 0.01, 0.004), recess, 0, 1.57, 0.203)
  for (const x of [-0.035, 0.035]) {
    part(`microphone-${x}`, new THREE.SphereGeometry(0.008, 12, 6), recess, x, -1.53, 0.202)
  }
  for (const x of [-1.022, 1.022]) {
    part(`antenna-seam-${x}`, new THREE.BoxGeometry(0.008, 0.028, 0.23), rear, x, 1.12, 0)
  }
  part('charging-port', box(0.24, 0.018, 0.07, 0.008), recess, 0, -1.754, 0)
  return model
}
