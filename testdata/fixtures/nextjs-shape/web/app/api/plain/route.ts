export function GET() {
  return new Response('ok');
}

export const HEAD = () => new Response(null, { status: 200, headers: { 'x-plain': '1' } });
