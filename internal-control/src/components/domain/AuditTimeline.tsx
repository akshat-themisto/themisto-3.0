import { formatDateTime, formatRelative } from "@/lib/format";

export function AuditTimeline({ actions }: { actions: any[] }) {
  return (
    <section className="timeline-card">
      <div className="card-title-row">
        <div>
          <h3>Control History</h3>
          <p>Every lifecycle action is written once and shown here as the source of truth.</p>
        </div>
      </div>
      <div className="timeline">
        {actions.length ? actions.map((action) => (
          <div key={action.id} className="timeline-item">
            <div className="timeline-dot" />
            <div className="timeline-content">
              <div className="timeline-title">
                {action.action_type.replace(/_/g, " ")} on {action.target_type}
              </div>
              <div className="timeline-meta">
                {action.actor_name || action.actor_email} - {formatDateTime(action.created_at)} - {formatRelative(action.created_at)}
              </div>
              {action.reason ? <div className="timeline-reason">{action.reason}</div> : null}
            </div>
          </div>
        )) : (
          <div className="empty-state compact-empty-state">
            <h3>No actions yet</h3>
            <p>The control history will populate as soon as operators start changing trust state.</p>
          </div>
        )}
      </div>
    </section>
  );
}
