import "./global.css"
import { RootProvider } from "@hanzo/ui/base/provider/next"
import type { ReactNode } from "react"

export const metadata = {
  title: {
    default: "Lux Indexer Documentation",
    template: "%s | Lux Indexer",
  },
  description: "Unified blockchain indexer for Lux Network",
}

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
    >
      <body className="min-h-svh bg-background font-sans antialiased">
        <RootProvider>
          <div className="relative flex min-h-svh flex-col bg-background">
            {children}
          </div>
        </RootProvider>
      </body>
    </html>
  )
}
