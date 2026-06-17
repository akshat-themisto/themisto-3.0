CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_external_org_id_unique
ON organizations(external_org_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_external_device_id_unique
ON devices(external_device_id);
