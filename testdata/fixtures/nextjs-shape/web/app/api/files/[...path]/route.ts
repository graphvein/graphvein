export async function GET(request: Request, context: { params: Promise<{ path: string[] }> }) {
  const { path } = await context.params;
  const body = await request.text();
  return Response.json({ path, size: body.length });
}
