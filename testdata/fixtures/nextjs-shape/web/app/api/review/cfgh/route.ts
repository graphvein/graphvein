// next.config sets a response header on this path (row SH-N24).
export async function GET() {
  return Response.json({ ok: 1 });
}
