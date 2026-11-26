// next.config sets a header whose name is not read on this path (row
// SH-N24): it may be Content-Type, so no content type is claimed.
export async function GET() {
  return Response.json({ ok: 1 });
}
