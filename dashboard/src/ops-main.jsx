import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import OpsApp from './OpsApp.jsx';

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <OpsApp />
  </StrictMode>,
);
