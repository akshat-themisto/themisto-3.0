import StatusDot from './StatusDot';
import { useAgentStatus } from '../hooks/useAgentStatus';

export default function StatusBar() {
  const { status } = useAgentStatus(3000);

  const running = status?.running;
  const gatewayConnected = status?.gateway_connected;
  const gatewayState = status?.gateway_state;
  const version = status?.version || '-';

  return (
    <div className="statusbar" data-tour="desktop-statusbar">
      <div className="statusbar-group">
        <StatusDot color={running ? 'green' : 'red'} />
        <span>{running ? 'Agent running' : 'Agent offline'}</span>
      </div>

      <div className="statusbar-group">
        <span>
          {gatewayConnected
            ? 'Gateway connected'
            : gatewayState === 'retrying'
              ? 'Gateway retrying'
              : gatewayState === 'starting'
                ? 'Gateway starting'
                : 'Gateway disconnected'}
        </span>
        <span className="statusbar-divider" />
        <span>Buffer {status?.buffer_size ?? 0}</span>
        <span className="statusbar-divider" />
        <span>v{version}</span>
      </div>
    </div>
  );
}
