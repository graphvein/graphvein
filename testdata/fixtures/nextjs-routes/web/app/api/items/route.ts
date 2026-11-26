export const runtime = 'nodejs';

export async function GET(request: Request) {
  return Response.json([]);
}

export function POST(request: Request) {
  return new Response(null, { status: 201 });
}

// Lowercase names are not route handlers.
export function get() {
  return null;
}
