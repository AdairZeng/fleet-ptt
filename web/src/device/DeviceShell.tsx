import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react'
import * as THREE from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { CSS3DObject, CSS3DRenderer } from 'three/examples/jsm/renderers/CSS3DRenderer.js'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { createPortal } from 'react-dom'
import { createDeviceModel, screenSize } from './model.ts'
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
  setPressed: (key: 'ptt' | 'command' | 'power', held: boolean) => void
  setIndicator: (color: DeviceSnapshot['indicator']) => void
  reset: () => void
  cleanup: () => void
}

const modelURL = '/models/xiaolu.glb'

export function DeviceShell({ children, status, onAction = () => {}, phase = 'standby', indicator }: DeviceShellProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const overlayRef = useRef<HTMLDivElement>(null)
  const [screenElement] = useState(() => document.createElement('div'))
  const [pressedKey, setPressedKey] = useState<'power' | 'command' | null>(null)
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
    const overlay = overlayRef.current
    if (!overlay) return
    const scene = createScene(canvas, overlay, screenElement, setModelState)
    sceneRef.current = scene
    return () => {
      scene.cleanup()
      sceneRef.current = null
    }
  }, [screenElement])

  useEffect(() => {
    sceneRef.current?.setIndicator(indicator)
  }, [indicator])

  useEffect(() => {
    sceneRef.current?.setPressed('ptt', pttHeld)
    sceneRef.current?.setPressed('power', pressedKey === 'power')
    sceneRef.current?.setPressed('command', pressedKey === 'command')
  }, [pttHeld, pressedKey])

  function trigger(action: DeviceAction) {
    actionRef.current(action)
  }

  function cancelPress() {
    if (pressTimer.current !== null) window.clearTimeout(pressTimer.current)
    pressTimer.current = null
    pressKind.current = null
    setPressedKey(null)
  }

  function beginPress(kind: 'power' | 'command') {
    if (pressKind.current !== null) return
    pressKind.current = kind
    setPressedKey(kind)
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
          <div ref={overlayRef} className="device-screen-overlay" />
          <div className="device-model-note" aria-live="polite">
            {modelState === 'model' ? '小路设备模型' : '小路设备灰模'}
          </div>
          <button
            type="button"
            className={`device-hotspot device-power-hotspot${pressedKey === 'power' ? ' held' : ''}`}
            aria-label="长按1.5秒开关机"
            {...pressHandlers('power')}
          >
            长按电源
          </button>
          <button
            type="button"
            className={`device-hotspot device-command-hotspot${pressedKey === 'command' ? ' held' : ''}`}
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
        {createPortal(
          <div className="device-screen" aria-label="小路设备屏幕">
            <div className="device-screen-status" role="status"><span className={`device-indicator indicator-${indicator}`} aria-hidden="true" />{status}</div>
            {children}
          </div>,
          screenElement,
        )}
        <button type="button" className="secondary device-reset" onClick={() => sceneRef.current?.reset()}>回正视角</button>

      </section>
    </div>
  )
}

function createScene(
  canvas: HTMLCanvasElement,
  overlay: HTMLDivElement,
  screenElement: HTMLDivElement,
  onModel: (state: 'model' | 'gray') => void,
): DeviceScene {
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  renderer.setClearColor(0x000000, 0)
  const scene = new THREE.Scene()
  const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 100)
  const fixedView = window.matchMedia('(max-width: 704px), (pointer: coarse)')
  const orbit = new OrbitControls(camera, canvas)
  orbit.target.set(0, 0.65, 0)
  orbit.enablePan = false
  orbit.enableZoom = false
  orbit.minPolarAngle = Math.PI / 3
  orbit.maxPolarAngle = Math.PI * 2 / 3
  const reset = () => {
    const distance = Math.max(7.8, 1.35 / (Math.tan(THREE.MathUtils.degToRad(16)) * camera.aspect))
    camera.position.set(distance * 0.16, 0.9, distance)
    orbit.enabled = !fixedView.matches
    orbit.update()
  }
  reset()
  fixedView.addEventListener('change', reset)
  scene.add(new THREE.HemisphereLight(0xf6f0e6, 0x1e293b, 2.2))
  const light = new THREE.DirectionalLight(0xffffff, 2.8)
  light.position.set(2, 4, 4)
  scene.add(light)

  let model: THREE.Object3D = createDeviceModel()
  scene.add(model)
  let disposed = false
  let indicator: DeviceSnapshot['indicator'] = 'blue'
  const held = { ptt: false, power: false, command: false }
  const positions = new Map<string, THREE.Vector3>()
  function bindModel() {
    positions.clear()
    for (const key of ['ptt', 'power', 'command']) {
      const node = model.getObjectByName(key)
      if (node) positions.set(key, node.position.clone())
    }
  }
  bindModel()
  new GLTFLoader().load(modelURL, (gltf) => {
    if (disposed) { disposeModel(gltf.scene); return }
    scene.remove(model)
    disposeModel(model)
    model = gltf.scene
    scene.add(model)
    bindModel()
    onModel('model')
  }, undefined, () => { if (!disposed) onModel('gray') })

  const cssRenderer = new CSS3DRenderer()
  overlay.append(cssRenderer.domElement)
  cssRenderer.domElement.style.pointerEvents = 'none'
  const cssScene = new THREE.Scene()
  const screen = new CSS3DObject(screenElement)
  screenElement.style.width = '240px'
  screenElement.style.height = '320px'
  screenElement.style.backfaceVisibility = 'hidden'
  screen.position.set(0, screenSize.y, screenSize.z)
  screen.scale.setScalar(screenSize.width / 240)
  cssScene.add(screen)

  const buttons = Object.fromEntries(['power', 'command', 'ptt'].map((key) => [
    key, canvas.parentElement!.querySelector<HTMLButtonElement>(`.device-${key}-hotspot`)!,
  ]))
  let width = 1
  let height = 1
  const resize = () => {
    const rect = canvas.getBoundingClientRect()
    width = Math.max(rect.width, 1)
    height = Math.max(rect.height, 1)
    renderer.setSize(width, height, false)
    cssRenderer.setSize(width, height)
    camera.aspect = width / height
    camera.updateProjectionMatrix()
    reset()
  }
  const observer = new ResizeObserver(resize)
  observer.observe(canvas)
  resize()
  let frame = 0
  const point = new THREE.Vector3()
  const animate = () => {
    if (disposed) return
    frame = window.requestAnimationFrame(animate)
    orbit.update()
    for (const key of ['power', 'command', 'ptt'] as const) {
      const node = model.getObjectByName(key)
      const base = positions.get(key)
      if (!node || !base) continue
      node.position.copy(base)
      if (held[key]) {
        if (key === 'power') node.position.y -= 0.06
        else node.position.x += key === 'ptt' ? 0.06 : -0.06
      }
      node.getWorldPosition(point)
      point.project(camera)
      const button = buttons[key]
      button.style.left = `${(point.x + 1) * width / 2}px`
      button.style.top = `${(1 - point.y) * height / 2}px`
      button.style.visibility = camera.position.z > 0 ? 'visible' : 'hidden'
    }
    const lamp = model.getObjectByName('indicator')
    if (lamp instanceof THREE.Mesh && lamp.material instanceof THREE.MeshStandardMaterial) {
      const color = { off: 0x334155, blue: 0x38bdf8, amber: 0xfbbf24, red: 0xf87171 }[indicator]
      lamp.material.color.setHex(color)
      lamp.material.emissive.setHex(color)
      lamp.material.emissiveIntensity = indicator === 'off' ? 0 : 0.8
    }
    const front = camera.position.z > screenSize.z
    screenElement.style.visibility = front ? 'visible' : 'hidden'
    screenElement.inert = !front
    renderer.render(scene, camera)
    cssRenderer.render(cssScene, camera)
  }
  animate()
  return {
    setPressed(key, value) { held[key] = value },
    setIndicator(value) { indicator = value },
    reset,
    cleanup() {
      disposed = true
      observer.disconnect()
      fixedView.removeEventListener('change', reset)
      window.cancelAnimationFrame(frame)
      orbit.dispose()
      cssScene.remove(screen)
      cssRenderer.domElement.remove()
      renderer.dispose()
      disposeModel(model)
    },
  }
}

function disposeModel(model: THREE.Object3D) {
  model.traverse((object) => {
    if (object instanceof THREE.Mesh) {
      object.geometry.dispose()
      const materials = Array.isArray(object.material) ? object.material : [object.material]
      for (const material of materials) material.dispose()
    }
  })
}
