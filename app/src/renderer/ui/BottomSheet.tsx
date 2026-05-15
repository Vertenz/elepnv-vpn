import * as Dialog from '@radix-ui/react-dialog'
import { createContext, useContext } from 'react'
import { type ReactNode } from 'react'

const ContainerContext = createContext<HTMLDivElement | null>(null)

export function BottomSheetContainerProvider({
  container,
  children,
}: {
  container: HTMLDivElement | null
  children: ReactNode
}) {
  return <ContainerContext.Provider value={container}>{children}</ContainerContext.Provider>
}

interface BottomSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  children: ReactNode
  labelledBy?: string
  describedBy?: string
}

export function BottomSheet({
  open,
  onOpenChange,
  children,
  labelledBy,
  describedBy,
}: BottomSheetProps) {
  const container = useContext(ContainerContext)

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal container={container}>
        <Dialog.Overlay className="sheet-overlay" />
        <Dialog.Content
          aria-describedby={describedBy}
          aria-labelledby={labelledBy}
          className="sheet"
        >
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

export { Dialog }
