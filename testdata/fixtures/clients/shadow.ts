import axios from './fake';
function fetch(url: string) {}
export function run() { fetch('/api/items'); axios.get('/items'); }
