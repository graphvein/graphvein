// An afterFiles rewrite runs before this dynamic route (row SH-N24).
export async function GET() {
  return Response.json({ ok: 1 }, { status: 201 });
}
