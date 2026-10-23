import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react'
import * as THREE from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { RoundedBoxGeometry } from 'three/examples/jsm/geometries/RoundedBoxGeometry.js'
import type { DevicePhase, DeviceSnapshot } from './controller.ts'

export type DeviceAction = 'power' | 'command' | 'command-cancel' | 'ptt-start' | 'ptt-end'

type DeviceShellProps = {
  children: ReactNode
  status: string
  onAction?: (action: DeviceAction) => void
  phase?: DevicePhase
  indicator: DeviceSnapshot['indicator']
}

type DeviceScene = {
  renderer: THREE.WebGLRenderer
  scene: THREE.Scene
  camera: THREE.PerspectiveCamera
  controls: THREE.Group
  frame: number
  cleanup: () => void
}

const modelURL = '/models/xiaolu.glb'

export function DeviceShell({ children, status, onAction = () => {}, phase = 'standby', indicator }: DeviceShellProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const [modelState, setModelState] = useState<'loading' | 'model' | 'gray'>('loading')
  const [pttHeld, setPttHeld] = useState(false)
  const pressTimer = useRef<number | null>(null)
  const pressKind = useRef<'power' | 'command' | null>(null)
  const longPressTriggered = useRef(false)
  const heldRef = useRef(false)
  const actionRef = useRef(onAction)
  const sceneRef = useRef<DeviceScene | null>(null)

  useEffect(() => {
    actionRef.current = onAction
  }, [onAction])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const scene = createScene(canvas, () => setModelState('gray'))
    sceneRef.current = scene

    const loader = new GLTFLoader()
    loader.load(
      modelURL,
      (gltf) => {
        scene.scene.add(gltf.scene)
        setModelState('model')
      },
      undefined,
      () => setModelState('gray'),
    )

    return () => {
      scene.cleanup()
      sceneRef.current = null
    }
  }, [])

  function trigger(action: DeviceAction) {
    actionRef.current(action)
  }

  function cancelPress() {
    if (pressTimer.current !== null) window.clearTimeout(pressTimer.current)
    pressTimer.current = null
    pressKind.current = null
  }

  function beginPress(kind: 'power' | 'command') {
    if (pressKind.current !== null) return
    pressKind.current = kind
    longPressTriggered.current = false
    pressTimer.current = window.setTimeout(() => {
      pressTimer.current = null
      longPressTriggered.current = true
      trigger(kind === 'power' ? 'power' : 'command-cancel')
    }, 1500)
  }

  function endPress(kind: 'power' | 'command') {
    if (pressKind.current !== kind) return
    const long = longPressTriggered.current
    cancelPress()
    if (kind === 'command' && !long) trigger('command')
  }

  function pressHandlers(kind: 'power' | 'command') {
    return {
      onPointerDown: (event: PointerEvent<HTMLButtonElement>) => {
        event.currentTarget.setPointerCapture(event.pointerId)
        beginPress(kind)
      },
      onPointerUp: () => endPress(kind),
      onPointerCancel: cancelPress,
      onLostPointerCapture: cancelPress,
      onBlur: cancelPress,
      onKeyDown: (event: import('react').KeyboardEvent<HTMLButtonElement>) => {
        if (event.key !== ' ' && event.key !== 'Enter') return
        event.preventDefault()
        if (!event.repeat) beginPress(kind)
      },
      onKeyUp: (event: import('react').KeyboardEvent<HTMLButtonElement>) => {
        if (event.key !== ' ' && event.key !== 'Enter') return
        event.preventDefault()
        endPress(kind)
      },
    }
  }

  function onPTTDown(event: PointerEvent<HTMLButtonElement>) {
    if (heldRef.current) return
    event.currentTarget.setPointerCapture(event.pointerId)
    heldRef.current = true
    setPttHeld(true)
    trigger('ptt-start')
  }

  function onPTTUp() {
    if (!heldRef.current) return
    heldRef.current = false
    setPttHeld(false)
    trigger('ptt-end')
  }

  useEffect(() => {
    const release = () => {
      cancelPress()
      if (!heldRef.current) return
      heldRef.current = false
      setPttHeld(false)
      actionRef.current('ptt-end')
    }
    const visibility = () => { if (document.hidden) release() }
    window.addEventListener('blur', release)
    document.addEventListener('visibilitychange', visibility)
    return () => {
      cancelPress()
      window.removeEventListener('blur', release)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [])

  useEffect(() => {
    if (phase !== 'listening' && phase !== 'talking' && heldRef.current) {
      heldRef.current = false
      setPttHeld(false)
      actionRef.current('ptt-end')
    }
  }, [phase])

  return (
    <div className="device-page">
      <section className="device-stage" aria-label="小路三维对讲机">
        <div className="device-canvas-wrap">
          <canvas ref={canvasRef} aria-hidden="true" />
          <div className="device-model-note" aria-live="polite">
            {modelState === 'model' ? '小路设备模型' : '小路设备灰模'}
          </div>
          <button
            type="button"
            className="device-hotspot device-power-hotspot"
            aria-label="长按1.5秒开关机"
            {...pressHandlers('power')}
          >
            长按电源
          </button>
          <button
            type="button"
            className="device-hotspot device-command-hotspot"
            aria-label="指令和结束键，长按取消"
            disabled={phase === 'off' || phase === 'booting'}
            {...pressHandlers('command')}
          >
            指令
          </button>
          <button
            type="button"
            className={`device-hotspot device-ptt-hotspot${pttHeld ? ' held' : ''}`}
            aria-label="按住说话"
            disabled={phase !== 'listening' && phase !== 'talking'}
            onPointerDown={onPTTDown}
            onPointerUp={onPTTUp}
            onPointerCancel={onPTTUp}
            onLostPointerCapture={onPTTUp}
            onBlur={onPTTUp}
            onPointerLeave={onPTTUp}
            onKeyDown={(event) => {
              if (event.repeat || (event.key !== ' ' && event.key !== 'Enter')) return
              event.preventDefault()
              if (heldRef.current) return
              heldRef.current = true
              setPttHeld(true)
              trigger('ptt-start')
            }}
            onKeyUp={(event) => {
              if (event.key !== ' ' && event.key !== 'Enter') return
              event.preventDefault()
              onPTTUp()
            }}
          >
            {pttHeld ? '松开结束' : '按住说话'}
          </button>
        </div>
        <div className="device-screen" aria-label="小路设备屏幕">
          <div className="device-screen-status" role="status"><span className={`device-indicator indicator-${indicator}`} aria-hidden="true" />{status}</div>
          {children}
        </div>
      </section>
    </div>
  )
}

function createScene(canvas: HTMLCanvasElement, onGray: () => void): DeviceScene {
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  renderer.setClearColor(0x000000, 0)
  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 100)
  camera.position.set(0.8, 0.9, 4.8)
  camera.lookAt(0, 0.3, 0)

  scene.add(new THREE.HemisphereLight(0xf6f0e6, 0x1e293b, 2.2))
  const key = new THREE.DirectionalLight(0xffffff, 2.8)
  key.position.set(2, 4, 4)
  scene.add(key)

  const controls = new THREE.Group()
  const body = new THREE.Mesh(
    new RoundedBoxGeometry(1.85, 3.1, 0.72, 5, 0.18),
    new THREE.MeshStandardMaterial({ color: 0x29313b, roughness: 0.7, metalness: 0.1 }),
  )
  body.position.y = 0.25
  controls.add(body)

  const screen = new THREE.Mesh(
    new THREE.BoxGeometry(1.25, 1.45, 0.05),
    new THREE.MeshStandardMaterial({ color: 0x102a43, emissive: 0x071a2b, emissiveIntensity: 0.8 }),
  )
  screen.position.set(0, 0.75, 0.39)
  controls.add(screen)

  const ptt = new THREE.Mesh(
    new THREE.BoxGeometry(0.2, 1.15, 0.3),
    new THREE.MeshStandardMaterial({ color: 0xd97706, roughness: 0.5 }),
  )
  ptt.position.set(-1.02, 0.35, 0)
  controls.add(ptt)

  const command = new THREE.Mesh(
    new THREE.BoxGeometry(0.2, 0.52, 0.26),
    new THREE.MeshStandardMaterial({ color: 0x0f766e, roughness: 0.5 }),
  )
  command.position.set(1.02, -0.2, 0)
  controls.add(command)

  const power = new THREE.Mesh(
    new THREE.CylinderGeometry(0.14, 0.14, 0.24, 24),
    new THREE.MeshStandardMaterial({ color: 0x111827, roughness: 0.4 }),
  )
  power.rotation.z = Math.PI / 2
  power.position.set(0.55, 1.9, 0.05)
  controls.add(power)
  scene.add(controls)

  const resize = () => {
    const rect = canvas.getBoundingClientRect()
    const width = Math.max(rect.width, 1)
    const height = Math.max(rect.height, 1)
    renderer.setSize(width, height, false)
    camera.aspect = width / height
    camera.updateProjectionMatrix()
  }
  const observer = new ResizeObserver(resize)
  observer.observe(canvas)
  resize()
  const animate = () => {
    const frame = window.requestAnimationFrame(animate)
    controls.rotation.y = Math.sin(performance.now() / 5000) * 0.04
    renderer.render(scene, camera)
    sceneState.frame = frame
  }
  const sceneState = { frame: 0 }
  animate()

  return {
    renderer,
    scene,
    camera,
    controls,
    frame: sceneState.frame,
    cleanup() {
      observer.disconnect()
      window.cancelAnimationFrame(sceneState.frame)
      renderer.dispose()
      scene.traverse((object) => {
        if (object instanceof THREE.Mesh) {
          object.geometry.dispose()
          const material = object.material
          if (Array.isArray(material)) material.forEach((item) => item.dispose())
          else material.dispose()
        }
      })
      onGray()
    },
  }
}
