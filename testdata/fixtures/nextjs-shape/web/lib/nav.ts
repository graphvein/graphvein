import { notFound } from 'next/navigation';

// notFound() in a followed function (row SH-N20): claimed only when no
// call site on the chain may catch it.
export async function gone(): Promise<Response> {
  notFound();
}

// relay reads the request, so all of its returns are recorded; its try
// block catches what gone() throws.
export async function relay(request: Request): Promise<Response> {
  const id = request.headers.get('x-relay');
  try {
    return await gone();
  } catch {
    return Response.json({ relayed: true, id }, { status: 502 });
  }
}
