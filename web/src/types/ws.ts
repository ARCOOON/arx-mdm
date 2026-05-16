export type TelemetryInstalledApp = {
  name: string
  version: string
  source: string
  id?: string
}

export type AssetRow = {
  id?: string
  human_id: string
  hostname: string
  os_type?: string
  os: string
  cpu_model: string
  cpu_logical_cores: number
  cpu_usage_percent: number
  total_ram_bytes: number
  memory_used_bytes: number
  last_seen?: string
  c2_connected: boolean
  installed_software?: TelemetryInstalledApp[]
}

export type TicketRow = {
  id?: string
  ticket_ref: string
  title: string
  status: string
  priority?: string
  linked_arx_id?: string
  created_at: string
}

export type AssetSnapshotMessage = {
  type: 'asset_snapshot'
  assets: AssetRow[]
}

export type TicketSnapshotMessage = {
  type: 'ticket_snapshot'
  tickets: TicketRow[]
}

export type TelemetryUpdateMessage = {
  type: 'telemetry_update'
  asset: AssetRow
}

export type CommandResultMessage = {
  type: 'command_result'
  ok: boolean
  message?: string
}

export type AndroidPolicyWire = {
  camera_disabled: boolean
  screen_lock_timeout_ms: number
  wipe_requested: boolean
}

export type AndroidPolicyUpdatedMessage = {
  type: 'android_policy_updated'
  asset_id: string
  human_id: string
  policy: AndroidPolicyWire
}

export type DeviceCommandWire = {
  id: string
  device_id: string
  command_type: string
  payload?: string
  status: string
  output?: string
  created_at: string
  completed_at?: string | null
  target_arx_id: string
}

export type DeviceCommandUpdateMessage = {
  type: 'device_command_update'
  command: DeviceCommandWire
}

export type RegistryResultMessage = {
  type: 'registry_result'
  target_arx_id: string
  request_id?: string
  ok: boolean
  error?: string
  key_path?: string
  value_name?: string
  value_type?: string
  data?: string
  message?: string
}

export type PtyOutputMessage = {
  type: 'pty_output'
  target_arx_id: string
  request_id?: string
  data_b64: string
}

export type PtyExitMessage = {
  type: 'pty_exit'
  target_arx_id: string
  request_id?: string
  code?: number
  error?: string
}

export type PtyStartedMessage = {
  type: 'pty_started'
  target_arx_id: string
  request_id?: string
  ok: boolean
  error?: string
}

export type PackageResultMessage = {
  type: 'package_result'
  target_arx_id: string
  deployment_id?: string
  request_id?: string
  ok: boolean
  error?: string
  operation?: string
  package_type?: string
}

export type InstallAppResultMessage = {
  type: 'install_app_result'
  target_arx_id: string
  app_id: string
  ok: boolean
  exit_code?: number
  error?: string
}

export type NetworkAddrWire = { addr: string }

export type NetworkInterfaceWire = {
  index: number
  name: string
  mtu: number
  hardware_addr?: string
  flags: string
  up: boolean
  loopback: boolean
  multicast: boolean
  addrs: NetworkAddrWire[]
}

export type FsListDirResultMessage = {
  type: 'fs_listdir_result'
  target_arx_id: string
  request_id?: string
  ok: boolean
  error?: string
  path?: string
  entries?: unknown[]
}

export type NetListResultMessage = {
  type: 'net_list_result'
  target_arx_id: string
  request_id?: string
  ok: boolean
  error?: string
  interfaces?: NetworkInterfaceWire[]
}

export type HostnameSetResultMessage = {
  type: 'hostname_set_result'
  target_arx_id: string
  request_id?: string
  ok: boolean
  error?: string
  hostname?: string
}

export type AgentUplinkMessage =
  | RegistryResultMessage
  | PtyOutputMessage
  | PtyExitMessage
  | PtyStartedMessage
  | PackageResultMessage
  | InstallAppResultMessage
  | FsListDirResultMessage
  | NetListResultMessage
  | HostnameSetResultMessage

export type ServerMessage =
  | AssetSnapshotMessage
  | TicketSnapshotMessage
  | TelemetryUpdateMessage
  | CommandResultMessage
  | AndroidPolicyUpdatedMessage
  | DeviceCommandUpdateMessage

export type ShutdownCommand = {
  action: 'shutdown'
  target_arx_id: string
}
