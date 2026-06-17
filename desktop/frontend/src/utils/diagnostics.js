// Dev-mode fallback for formatting diagnostics text.
// In production, use BuildDiagnosticsReport() Go binding instead.
export function formatDiagnosticsText(snapshot) {
  if (!snapshot) return '(no diagnostics data)';

  const lines = [];
  lines.push('=== Themisto Diagnostics Report ===');
  lines.push(`Generated: ${snapshot.timestamp || '(unknown)'}`);
  lines.push(`App Version: ${snapshot.app_version || '-'}`);
  lines.push(`Overall Status: ${snapshot.summary_status || '-'}`);

  lines.push('');
  lines.push('--- System ---');
  lines.push(`Hostname: ${snapshot.system?.hostname || '-'}`);
  lines.push(`OS: ${snapshot.system?.os || '-'}`);

  lines.push('');
  lines.push('--- Agent Status ---');
  lines.push(`Running: ${snapshot.status?.running ?? '-'}`);
  lines.push(`Agent ID: ${snapshot.status?.agent_id || '-'}`);
  lines.push(`Gateway Connected: ${snapshot.status?.gateway_connected ?? '-'}`);
  lines.push(`Gateway State: ${snapshot.status?.gateway_state || '-'}`);
  lines.push(`Proxy State: ${snapshot.status?.proxy_state || '-'}`);
  lines.push(`Proxy Listener Ready: ${snapshot.status?.proxy_listener_ready ?? '-'}`);
  lines.push(`Runtime Phase: ${snapshot.status?.runtime_phase || '-'}`);
  lines.push(`Uptime: ${snapshot.status?.uptime || '-'}`);

  lines.push('');
  lines.push('--- Agent Configuration ---');
  lines.push(`Gateway URL: ${snapshot.config?.gateway_url || '-'}`);
  lines.push(`Organization: ${snapshot.config?.org_name || '-'}`);
  lines.push(`Enrolled: ${snapshot.config?.enrolled ?? '-'}`);
  lines.push(`Enrollment Health: ${snapshot.config?.enrollment_health || '-'}`);

  lines.push('');
  lines.push('--- Installation State ---');
  lines.push(`Summary: ${snapshot.installed?.summary || '-'}`);
  lines.push(`Agent Service: ${snapshot.service_state || '-'}`);

  lines.push('');
  lines.push('--- File Paths ---');
  for (const pc of snapshot.path_checks || []) {
    lines.push(`${pc.exists ? '[OK]     ' : '[MISSING]'} ${pc.path}`);
  }

  lines.push('');
  lines.push('--- Issues ---');
  if (!snapshot.issues?.length) {
    lines.push('(none)');
  } else {
    for (const issue of snapshot.issues) {
      lines.push(`[${issue.severity?.toUpperCase()}] ${issue.title} — ${issue.detail}`);
    }
  }

  lines.push('');
  lines.push('--- Collection Errors ---');
  if (!snapshot.collection_errors?.length) {
    lines.push('(none)');
  } else {
    for (const ce of snapshot.collection_errors) {
      lines.push(`- ${ce}`);
    }
  }

  return lines.join('\n');
}
