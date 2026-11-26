import { Link, Outlet } from 'react-router';
import * as UI from './ui';

export function Layout() {
  // Link and Outlet are external components (D); UI.Button is JR2 through
  // a namespace import.
  return (
    <>
      <Link to="/">home</Link>
      <UI.Button />
      <Outlet />
    </>
  );
}
