UPDATE telemetry_events te
SET org_id = o.name
FROM organizations o
WHERE te.org_id = o.id::text
  AND te.org_id IS DISTINCT FROM o.name;

UPDATE dlp_events de
SET org_id = o.name
FROM organizations o
WHERE de.org_id = o.id::text
  AND de.org_id IS DISTINCT FROM o.name;
