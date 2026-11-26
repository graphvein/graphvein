// next.config redirects ?go=1 on this path before the handler (row SH-N24).
export async function GET() {
  return Response.json({ ok: 1 }, { status: 201 });
}
