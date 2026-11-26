// next.config sets Content-Length on this path (rows SH-N24, SH-N30): the
// body is cut short, so no content type or body is claimed.
export async function GET() {
  return Response.json({ ok: 12345 });
}
