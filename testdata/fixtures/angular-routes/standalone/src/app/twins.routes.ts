import HomePage from './pages/home';
import { DocsPage } from './pages/docs';

const first = [{ path: 'twin', component: HomePage }];
const second = [{ path: 'twin', component: DocsPage }];

export { first as twinFirst };
export default second;
