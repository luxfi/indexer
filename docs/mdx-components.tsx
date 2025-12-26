import type { MDXComponents } from "mdx/types"
import { defaultMdxComponents } from "@hanzo/ui"

export function useMDXComponents(components: MDXComponents): MDXComponents {
  return {
    ...defaultMdxComponents,
    ...components,
  }
}
