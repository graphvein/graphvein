import { createRoot } from 'react-dom/client';
import App from './App';

// JR2 from module scope: the file renders App (default export).
createRoot(document.getElementById('root')!).render(<App />);
