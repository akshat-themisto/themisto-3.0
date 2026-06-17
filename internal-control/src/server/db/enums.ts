export type StaffRole = "owner" | "admin" | "operator" | "viewer";
export type BillingStatus = "active" | "overdue" | "churned";
export type ContractStatus = "trial" | "active" | "past_due" | "ended";
export type OrgStatus = "active" | "suspended" | "terminated";
export type DeviceStatus = "active" | "disabled" | "revoked";
export type DeploymentHealth = "healthy" | "degraded" | "down" | "unknown";
export type DeploymentEnvironment = "production" | "staging" | "development";
export type OperatingSystem = "windows" | "macos" | "linux";
export type ActionType =
  | "create"
  | "update"
  | "suspend"
  | "reactivate"
  | "terminate"
  | "disable_device"
  | "revoke_device"
  | "reactivate_device"
  | "revoke_all_devices"
  | "mark_contract_ended"
  | "mark_overdue"
  | "mark_churned"
  | "staff_create"
  | "staff_deactivate"
  | "sign_in";
export type TargetType = "customer" | "organization" | "device" | "contract" | "deployment" | "staff";

