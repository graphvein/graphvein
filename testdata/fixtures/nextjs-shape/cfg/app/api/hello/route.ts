export async function GET() {
  return Response.json({ hello: true });
}

export async function POST() {
  return Response.json({ created: true }, { status: 201 });
}
