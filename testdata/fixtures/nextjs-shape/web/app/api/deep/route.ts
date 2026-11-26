import { c1, r1, bare } from '../../../lib/chain';

export async function GET(request: Request) {
  c1(request);
  if (request.headers.has('x-bare')) {
    return bare();
  }
  return r1('ok');
}
