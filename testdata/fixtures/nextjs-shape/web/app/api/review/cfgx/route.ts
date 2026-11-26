// next.config sets X-Frame-Options and Cache-Control on this path from a
// header list grown with push() (rows SH-N24, SH-N30): the JSON content
// type stays.
export async function GET() {
  return Response.json({ ok: 1 });
}
