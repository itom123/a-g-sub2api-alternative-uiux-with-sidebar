import { onScopeDispose, watch, type Ref } from 'vue'

const locks = new Set<symbol>()

/** One lock per overlay owner, released only after the last overlay closes. */
export function useModalScrollLock(open: Ref<boolean>) {
  const owner = Symbol('overlay')
  const release = () => {
    locks.delete(owner)
    document.body.classList.toggle('modal-open', locks.size > 0)
  }
  watch(open, (visible) => {
    if (visible) locks.add(owner)
    else locks.delete(owner)
    document.body.classList.toggle('modal-open', locks.size > 0)
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(release)
}
