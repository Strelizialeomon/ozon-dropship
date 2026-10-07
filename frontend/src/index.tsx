import React from 'react';
import ReactDOM from 'react-dom/client';
import './styles/index.css';
import Router from '@/router';

const rootEl = document.getElementById('root');
if (rootEl) {
  ReactDOM.createRoot(rootEl).render(
    <React.StrictMode>
      <Router />
    </React.StrictMode>,
  );
}
