// An afterFiles rewrite matches this static path; Next.js serves the
// route first (row SH-N24).
export async function GET() {
  return Response.json({ ok: 1 }, { status: 201 });
}
