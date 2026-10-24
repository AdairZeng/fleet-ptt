import { useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react'
import * as THREE from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { CSS3DObject, CSS3DRenderer } from 'three/examples/jsm/renderers/CSS3DRenderer.js'
import { RoomEnvironment } from 'three/examples/jsm/environments/RoomEnvironment.js'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { createPortal } from 'react-dom'
import { createDeviceModel, screenSize, screenPixels } from './model.ts'
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

const modelURL = '/models/xiaolu.glb?v=5'

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
      <header className="product-heading">
        <p className="product-eyebrow">XIAOLU</p>
        <h1>随声，同行。</h1>
        <p>小路 · 随身语音伙伴</p>
      </header>
      <section className="device-stage" aria-label="小路三维语音终端">
        <div className="device-canvas-wrap">
          <canvas ref={canvasRef} aria-hidden="true" />
          <div ref={overlayRef} className="device-screen-overlay" />
          {modelState === 'loading' ? <div className="device-model-note" role="status">正在加载设备</div> : null}
          <button
            type="button"
            className={`device-hotspot device-power-hotspot${pressedKey === 'power' ? ' held' : ''}`}
            aria-label="长按1.5秒开关机"
            {...pressHandlers('power')}
          >
            <svg aria-hidden="true" viewBox="0 0 24 24"><path d="M12 3v8M6.4 6.4a8 8 0 1 0 11.2 0" /></svg><small>长按开关</small>
          </button>
          <button
            type="button"
            className={`device-hotspot device-command-hotspot${pressedKey === 'command' ? ' held' : ''}`}
            aria-label="指令和结束键，长按取消"
            disabled={phase === 'off' || phase === 'booting'}
            {...pressHandlers('command')}
          >
            <span aria-hidden="true" className="voice-key-icon"><i /><i /><i /><i /><i /></span><small>小路</small>
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
            <svg aria-hidden="true" viewBox="0 0 24 24"><rect x="9" y="3" width="6" height="12" rx="3" /><path d="M6 11v1a6 6 0 0 0 12 0v-1M12 18v3M9 21h6" /></svg><small>{pttHeld ? '松开结束' : '按住说话'}</small>
          </button>
        </div>
        {createPortal(
          <div className={`device-screen${phase === 'off' ? ' device-screen-off' : ''}`} aria-label="小路设备屏幕">
            <div className="device-screen-status" role="status"><span className={`device-indicator indicator-${indicator}`} aria-hidden="true" />{status}</div>
            {children}
          </div>,
          screenElement,
        )}
        <div className="device-stage-footer"><span>为每一次同行而连接</span><button type="button" className="secondary device-reset" onClick={() => sceneRef.current?.reset()}>回正视角</button></div>

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
  renderer.toneMapping = THREE.ACESFilmicToneMapping
  renderer.toneMappingExposure = 1.05
  const scene = new THREE.Scene()
  const studio = new RoomEnvironment()
  const pmrem = new THREE.PMREMGenerator(renderer)
  const environment = pmrem.fromScene(studio, 0.04)
  scene.environment = environment.texture
  scene.environmentIntensity = 0.9
  studio.dispose()
  pmrem.dispose()
  const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 100)
  const fixedView = window.matchMedia('(max-width: 704px), (pointer: coarse)')
  const orbit = new OrbitControls(camera, canvas)
  orbit.target.set(0, 0, 0)
  orbit.enablePan = false
  orbit.enableZoom = false
  orbit.minPolarAngle = Math.PI / 3
  orbit.maxPolarAngle = Math.PI * 2 / 3
  const reset = () => {
    const distance = Math.max(7.5, 1.38 / (Math.tan(THREE.MathUtils.degToRad(16)) * camera.aspect))
    camera.position.set(distance * (fixedView.matches ? 0.04 : 0.16), 0.12, distance)
    orbit.enabled = !fixedView.matches
    orbit.update()
  }
  reset()
  fixedView.addEventListener('change', reset)
  scene.add(new THREE.HemisphereLight(0xfffaf2, 0x7b8490, 1.4))
  const light = new THREE.DirectionalLight(0xfffaf4, 2.1)
  light.position.set(-3, 5, 5)
  scene.add(light)
  const rim = new THREE.DirectionalLight(0xdceaff, 1.5)
  rim.position.set(3, 1, -2)
  scene.add(rim)

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
  // Hidden overflow can scroll on focus and offset CSS3D independently of WebGL.
  cssRenderer.domElement.style.overflow = 'clip'
  cssRenderer.domElement.style.position = 'absolute'
  cssRenderer.domElement.style.inset = '0'
  const cssScene = new THREE.Scene()
  const screen = new CSS3DObject(screenElement)
  screenElement.style.width = `${screenPixels.width}px`
  screenElement.style.top = '0'
  screenElement.style.left = '0'
  screenElement.style.height = `${screenPixels.height}px`
  screenElement.style.overflow = 'clip'
  screenElement.style.backfaceVisibility = 'hidden'
  screen.position.set(0, screenSize.y, screenSize.z)
  screen.scale.setScalar(screenSize.width / screenPixels.width)
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
        node.position.z -= 0.012
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
      const color = { off: 0x788581, blue: 0x9bcbd3, amber: 0xfbbf24, red: 0xf87171 }[indicator]
      lamp.material.color.setHex(color)
      lamp.material.emissive.setHex(color)
      lamp.material.emissiveIntensity = indicator === 'off' ? 0 : 0.5
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
      environment.dispose()
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
