import run from './default';
export function Widget() { return <p>{run()}</p>; }
export default class { method() { return run(); } }
