async function update(request: Request) {
  return Response.json({ updated: true });
}

async function remove(request: Request) {
  return new Response(null, { status: 204 });
}

export const PUT = update;
export const PATCH = async (request: Request) => Response.json({ patched: true });
export { remove as DELETE };
