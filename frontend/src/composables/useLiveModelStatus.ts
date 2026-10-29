import { onMounted, onBeforeUnmount } from 'vue'
import { useModelStatusStore } from '@/stores/modelStatus'
export function useLiveModelStatus() {
  const status = useModelStatusStore()
  onMounted(status.retain)
  onBeforeUnmount(status.release)
  return status
}
