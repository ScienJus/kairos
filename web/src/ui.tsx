import type { ReactNode } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { X, XCircle } from 'lucide-react'
import { useI18n, type MessageKey } from './i18n'

export function formValue(data: FormData, name: string) { return String(data.get(name) ?? '') }

export function splitValues(value: FormDataEntryValue | null) {
  return String(value ?? '').split(',').map(item => item.trim()).filter(Boolean)
}

export function Status({ value }: { value: string }) {
  const { t } = useI18n()
  const labels: Record<string, MessageKey> = {
    open: 'statusOpen', completed: 'statusCompleted', cancelled: 'statusCancelled', failed: 'statusFailed', awaiting_agent_acceptance: 'statusAwaitingAgentAcceptance', awaiting_human_acceptance: 'statusAwaitingHumanAcceptance',
    pending: 'statusPending', working: 'statusWorking', waiting_children: 'statusWaiting', in_review: 'statusReview', skipped: 'statusSkipped', not_reached: 'statusNotReached',
  }
  const key = labels[value]
  return <span className={`status status-${value}`}><i />{key ? t(key) : value.toUpperCase()}</span>
}

export function Modal({ open, onOpenChange, title, eyebrow, children }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; eyebrow: string; children: ReactNode
}) {
  const { t } = useI18n()
  return <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Overlay className="dialog-overlay" />
      <Dialog.Content className="dialog-content">
        <div className="dialog-heading"><div><span className="eyebrow">{eyebrow}</span><Dialog.Title>{title}</Dialog.Title></div>
          <Dialog.Close className="icon-button" aria-label={t('close')}><X size={17} /></Dialog.Close></div>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
}

export function FormError({ error }: { error: Error }) {
  return <div className="form-error wide"><XCircle size={15} />{error.message}</div>
}
