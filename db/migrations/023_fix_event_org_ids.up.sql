UPDATE telemetry_events te
SET org_id = d.org_id::text
FROM devices d
WHERE te.device_id = d.id
  AND te.org_id IS DISTINCT FROM d.org_id::text;

UPDATE telemetry_events te
SET org_id = o.id::text
FROM organizations o
WHERE te.org_id = o.name
  AND te.org_id IS DISTINCT FROM o.id::text;

UPDATE dlp_events de
SET org_id = d.org_id::text
FROM devices d
WHERE de.device_id = d.id
  AND de.org_id IS DISTINCT FROM d.org_id::text;

UPDATE dlp_events de
SET org_id = o.id::text
FROM organizations o
WHERE de.org_id = o.name
  AND de.org_id IS DISTINCT FROM o.id::text;
