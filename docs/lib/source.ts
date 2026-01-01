import { docs } from "@/.source/server"
import { loader } from "@hanzo/docs/source"

// Create a single source instance that is reused
// This prevents circular references and stack overflow issues
let _source: ReturnType<typeof loader> | null = null

export function getSource() {
  if (!_source) {
    _source = loader({
      baseUrl: "/docs",
      source: docs.toFumadocsSource(),
    })
  }
  return _source
}

export const source = getSource()
