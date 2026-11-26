// next.config sets a Content-Type header on this path (row SH-N24): no
// content type is claimed.
export async function GET() {
  return Response.json({ ok: 1 });
}

// A response without a body: the config Content-Type is the only one.
export async function POST() {
  return new Response(null, { status: 202 });
}
