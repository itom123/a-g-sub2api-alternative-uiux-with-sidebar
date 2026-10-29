import { apiClient } from './client'

// Allow the bounded five-minute worker/bootstrap operation to return its result.
const lifecycleTimeout = 330_000

export type OpenClawConnectionKind = string

export interface OpenClawConnectionKindInfo {
  kind: OpenClawConnectionKind
  title: string
  description: string
  sort_order: number
  setup_available: boolean
}

export interface OpenClawInstance {
  id: number
  tenant_slug: string
  name: string
  status: string
  worker_host: string
  port?: number
  last_error?: string
  created_at: string
  updated_at: string
  deletion_requested_at?: string
  deletion_scheduled_at?: string
}

export interface OpenClawLifecycle {
  instance?: OpenClawInstance
  recreate_allowed_at?: string
  hub_name?: string
  can_create: boolean
  can_delete?: boolean
  self_service_enabled?: boolean
  create_request_pending?: boolean
  delete_request_pending?: boolean
}

export interface OpenClawAccessRequest {
  id: number
  user_id: number
  user_email?: string
  action: 'create' | 'delete'
  instance_id?: number
  reason: string
  status: 'pending' | 'approved' | 'rejected' | 'consumed'
  reviewed_by?: number
  reviewed_at?: string
  review_note: string
  created_at: string
  updated_at: string
}

export interface OpenClawAccessPolicy {
  user_id: number
  self_service_enabled: boolean
  hub_name: string
}

export interface OpenClawConnection {
  id: number
  instance_id: number
  kind: OpenClawConnectionKind
  status: string
  label: string
  usage_count: number
  last_used_at?: string
  created_at: string
  updated_at: string
  setup_url?: string
  setup_key?: string
  setup_message?: string
}

export interface OpenClawDashboard {
  gateway_enabled?: boolean
  enrollment_enabled?: boolean
  instance: OpenClawInstance
  fleet_state: string
  latency_ms?: number
  checked_at: string
  connection_count: number
  connected_count: number
  usage_count: number
  last_used_at?: string
  api_key?: OpenClawAPIKey
}

export interface OpenClawGatewayDevice {
  deviceId: string
  requestId?: string
  displayName?: string
  operatorLabel?: string
  clientId?: string
  platform?: string
  role?: string
  roles?: string[]
  scopes?: string[]
  approvedScopes?: string[]
  ts?: number
  approvedAtMs?: number
  lastSeenAtMs?: number
}

export interface OpenClawGatewayInventory {
  pending: OpenClawGatewayDevice[]
  paired: OpenClawGatewayDevice[]
}

export async function getOpenClawGatewayDevices(id: number): Promise<OpenClawGatewayInventory> {
  const { data } = await apiClient.get<{ devices: OpenClawGatewayInventory }>(`/openclaw/instances/${id}/gateway-devices`)
  return data.devices
}

export async function updateOpenClawGatewayDevice(id: number, action: 'approve' | 'reject' | 'remove', device: OpenClawGatewayDevice): Promise<void> {
  await apiClient.post(`/openclaw/instances/${id}/gateway-devices`, { action, device_id: device.deviceId, request_id: device.requestId }, { timeout: 30_000 })
}

export interface OpenClawGatewayEnrollment {
  id: string
  client: 'ios' | 'macos'
  state: string
  setup_code: string
  gateway_url: string
  created_at: string
  expires_at: string
  delivery_uncertain?: boolean
  device?: {
    request_id: string
    device_id: string
      display_name?: string
      platform?: string
    roles: string[]
    scopes: string[]
  }
}

export async function createOpenClawGatewayEnrollment(id: number, client: 'ios' | 'macos'): Promise<OpenClawGatewayEnrollment> {
  const { data } = await apiClient.post<OpenClawGatewayEnrollment>(`/openclaw/instances/${id}/gateway-enrollment`, { client }, { timeout: 30_000 })
  return data
}

export async function getOpenClawGatewayEnrollment(id: number, enrollmentId: string): Promise<OpenClawGatewayEnrollment> {
  const { data } = await apiClient.get<OpenClawGatewayEnrollment>(`/openclaw/instances/${id}/gateway-enrollment`, { params: { id: enrollmentId }, timeout: 15_000 })
  return data
}

export async function cancelOpenClawGatewayEnrollment(id: number, enrollmentId: string): Promise<void> {
  await apiClient.delete(`/openclaw/instances/${id}/gateway-enrollment`, { params: { id: enrollmentId }, timeout: 15_000 })
}

export interface OpenClawAPIKey {
  id: number
  name: string
  status: string
  quota: number
  quota_used: number
  usage_count: number
  usage_cost: number
  last_used_at?: string
}

export interface OpenClawAdminInstance extends OpenClawInstance {
  user_id: number
  user_email: string
  billing_group: string
  billing_rate: number
  api_key_status: string
  usage_cost: number
  allowed_models: string[]
  default_model: string
  mapping_enabled: boolean
  connection_count: number
  connected_count: number
  usage_count: number
  last_used_at?: string
  last_latency_ms?: number
  last_seen_at?: string
}

interface ListResponse<T> {
  items: T[]
}

export async function listOpenClawInstances(): Promise<OpenClawInstance[]> {
  const { data } = await apiClient.get<ListResponse<OpenClawInstance>>('/openclaw/instances')
  return data.items
}

export async function getOpenClawLifecycle(): Promise<OpenClawLifecycle> {
  const { data } = await apiClient.get<OpenClawLifecycle>('/openclaw/lifecycle')
  return data
}

export async function getOpenClawDashboard(): Promise<OpenClawDashboard> {
  const { data } = await apiClient.get<OpenClawDashboard>('/openclaw/dashboard')
  return data
}

export async function listOpenClawConnectionKinds(): Promise<OpenClawConnectionKindInfo[]> {
  const { data } = await apiClient.get<ListResponse<OpenClawConnectionKindInfo>>('/openclaw/catalog')
  return data.items
}

export async function createOpenClawInstance(name: string): Promise<OpenClawInstance> {
  const { data } = await apiClient.post<OpenClawInstance>('/openclaw/instances', { name }, { timeout: lifecycleTimeout })
  return data
}

export async function createOpenClawAccessRequest(action: 'create' | 'delete', instanceId?: number, reason = ''): Promise<OpenClawAccessRequest> {
  const { data } = await apiClient.post<OpenClawAccessRequest>('/openclaw/access-requests', {
    action,
    ...(instanceId ? { instance_id: instanceId } : {}),
    reason,
  })
  return data
}

export async function startOpenClawInstance(id: number): Promise<void> {
  await apiClient.post(`/openclaw/instances/${id}/start`, undefined, { timeout: lifecycleTimeout })
}

export async function stopOpenClawInstance(id: number): Promise<void> {
  await apiClient.post(`/openclaw/instances/${id}/stop`, undefined, { timeout: lifecycleTimeout })
}

export async function getOpenClawGatewayLaunch(id: number): Promise<{ url: string }> {
  const { data } = await apiClient.post<{ url: string }>(`/openclaw/instances/${id}/gateway-launch`)
  return data
}

export async function renameOpenClawInstance(id: number, name: string): Promise<OpenClawInstance> {
  const { data } = await apiClient.patch<OpenClawInstance>(`/openclaw/instances/${id}`, { name })
  return data
}

export async function provisionOpenClawInstance(id: number): Promise<OpenClawInstance> {
  const { data } = await apiClient.post<OpenClawInstance>(`/openclaw/instances/${id}/provision`, undefined, { timeout: lifecycleTimeout })
  return data
}

export async function deleteOpenClawInstance(id: number): Promise<OpenClawInstance> {
  const { data } = await apiClient.delete<OpenClawInstance>(`/openclaw/instances/${id}`, { timeout: lifecycleTimeout })
  return data
}

export async function listOpenClawConnections(id: number): Promise<OpenClawConnection[]> {
  const { data } = await apiClient.get<ListResponse<OpenClawConnection>>(`/openclaw/instances/${id}/connections`)
  return data.items
}

export async function createOpenClawConnection(
  instanceId: number,
  kind: OpenClawConnectionKind,
  label = '',
  credential = ''
): Promise<OpenClawConnection> {
  const { data } = await apiClient.post<OpenClawConnection>(`/openclaw/instances/${instanceId}/connections`, { kind, label, credential })
  return data
}

export async function completeOpenClawConnection(
  instanceId: number,
  connectionId: number,
  credential: string
): Promise<OpenClawConnection> {
  const { data } = await apiClient.post<OpenClawConnection>(
    `/openclaw/instances/${instanceId}/connections/${connectionId}/complete`,
    { credential }
  )
  return data
}

export async function deleteOpenClawConnection(instanceId: number, connectionId: number): Promise<void> {
  await apiClient.delete(`/openclaw/instances/${instanceId}/connections/${connectionId}`)
}

export async function listAdminOpenClawInstances(): Promise<OpenClawAdminInstance[]> {
  const { data } = await apiClient.get<ListResponse<OpenClawAdminInstance>>('/admin/openclaw/instances')
  return data.items
}

export async function deleteAdminOpenClawInstance(id: number): Promise<OpenClawInstance> {
  const { data } = await apiClient.delete<OpenClawInstance>(`/admin/openclaw/instances/${id}`)
  return data
}

export async function reconcileAdminOpenClawInstance(id: number): Promise<OpenClawInstance> {
  const { data } = await apiClient.post<OpenClawInstance>(`/admin/openclaw/instances/${id}/reconcile`)
  return data
}

export async function listAdminOpenClawAccessRequests(status = 'pending'): Promise<OpenClawAccessRequest[]> {
  const { data } = await apiClient.get<ListResponse<OpenClawAccessRequest>>('/admin/openclaw/access-requests', { params: { status } })
  return data.items
}

export async function reviewAdminOpenClawAccessRequest(id: number, approved: boolean, note = ''): Promise<OpenClawAccessRequest> {
  const { data } = await apiClient.post<OpenClawAccessRequest>(`/admin/openclaw/access-requests/${id}/decision`, { approved, note })
  return data
}

export async function setAdminOpenClawAccessPolicy(userId: number, selfServiceEnabled: boolean): Promise<OpenClawAccessPolicy> {
  const { data } = await apiClient.put<OpenClawAccessPolicy>(`/admin/openclaw/users/${userId}/policy`, { self_service_enabled: selfServiceEnabled })
  return data
}

export interface CareerProfile {
  user_id: number
  display_name: string
  roles: string
  locations: string
  remote_ok: boolean
  salary_min: number
  notes: string
  timezone: string
  telegram_paired: boolean
  telegram_paired_at?: string
  updated_at: string
}

export interface CareerSchedule {
  id: number
  user_id: number
  cron_expression: string
  timezone: string
  enabled: boolean
  openclaw_job_id?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export async function getCareerProfile(): Promise<CareerProfile> {
  const { data } = await apiClient.get<CareerProfile>('/openclaw/career/profile')
  return data
}

export async function updateCareerProfile(input: Omit<CareerProfile, 'user_id' | 'telegram_paired' | 'telegram_paired_at' | 'updated_at'>): Promise<CareerProfile> {
  const { data } = await apiClient.put<CareerProfile>('/openclaw/career/profile', input)
  return data
}

export async function createCareerPairing(): Promise<{ code: string; expires_at: string }> {
  const { data } = await apiClient.post<{ code: string; expires_at: string }>('/openclaw/career/pairing')
  return data
}

export async function unpairCareerTelegram(): Promise<void> {
  await apiClient.delete('/openclaw/career/pairing')
}

export async function listCareerSchedules(): Promise<CareerSchedule[]> {
  const { data } = await apiClient.get<ListResponse<CareerSchedule>>('/openclaw/career/schedules')
  return data.items
}

export async function createCareerSchedule(input: { cron_expression: string; timezone: string; enabled?: boolean }): Promise<CareerSchedule> {
  const { data } = await apiClient.post<CareerSchedule>('/openclaw/career/schedules', input)
  return data
}

export async function deleteCareerSchedule(id: number): Promise<void> {
  await apiClient.delete(`/openclaw/career/schedules/${id}`)
}
