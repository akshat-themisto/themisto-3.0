export default function StatusDot({ color = 'gray', size }) {
  const style = size ? { width: size, height: size } : {};
  return <span className={`status-dot ${color}`} style={style} />;
}
