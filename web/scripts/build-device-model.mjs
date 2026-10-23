import { writeFile } from 'node:fs/promises'
import { GLTFExporter } from 'three/examples/jsm/exporters/GLTFExporter.js'
import { createDeviceModel } from '../src/device/model.ts'

// GLTFExporter uses the browser FileReader API to package its binary buffer.
globalThis.FileReader = class {
  readAsArrayBuffer(blob) {
    void blob.arrayBuffer().then((result) => {
      this.result = result
      this.onloadend?.()
    })
  }
}
const binary = await new GLTFExporter().parseAsync(createDeviceModel(), { binary: true })
await writeFile(new URL('../public/models/xiaolu.glb', import.meta.url), Buffer.from(binary))
