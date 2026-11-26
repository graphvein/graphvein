export async function GET(request: Request) {
  return Response.json({ items: [] });
}

export async function POST(request: Request) {
  return Response.json({ ok: true });
}
